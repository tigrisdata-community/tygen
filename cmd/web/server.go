package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/a-h/templ"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"github.com/openai/openai-go"
	"github.com/rbcervilla/redisstore/v9"
	"github.com/tigrisdata-community/tygen/models"
	"github.com/tigrisdata-community/tygen/web"
)

const sessionName = "session"

var upgrader = websocket.Upgrader{}

type Options struct {
	DatabaseURL           string
	RedisURL              string
	S3Client              *s3.Client
	ReferenceImagesBucket string
	TigrisBucket          string
}

func New(opts Options) (*Server, error) {
	rdb, err := models.ConnectValkey(opts.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("can't connect to valkey: %w", err)
	}

	dao, err := models.New(opts.DatabaseURL, rdb)
	if err != nil {
		return nil, fmt.Errorf("can't create DAO: %w", err)
	}

	store, err := redisstore.NewRedisStore(context.Background(), rdb)
	if err != nil {
		return nil, fmt.Errorf("can't create redis store: %w", err)
	}

	oai := openai.NewClient()

	store.KeyPrefix("session:")
	store.Options(sessions.Options{
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400 * 60,
	})

	result := &Server{
		dao:                   dao,
		store:                 store,
		s3c:                   opts.S3Client,
		oai:                   &oai,
		referenceImagesBucket: opts.ReferenceImagesBucket,
		tigrisBucket:          opts.TigrisBucket,
	}

	return result, nil
}

type Server struct {
	dao                   *models.DAO
	store                 *redisstore.RedisStore
	s3c                   *s3.Client
	oai                   *openai.Client
	referenceImagesBucket string
	tigrisBucket          string
}

func (s *Server) register(mux *http.ServeMux) {
	web.Mount(mux)
	mux.HandleFunc("/{$}", s.Index)
	mux.HandleFunc("/", s.NotFound)
	mux.HandleFunc("POST /submit", s.Submit)
	mux.HandleFunc("GET /imagegen/{id}/status", s.ImagegenStatus)
}

func (s *Server) Index(w http.ResponseWriter, r *http.Request) {
	flashes, _ := getFlashes(r.Context())

	templ.Handler(web.Simple("Tygen", web.QuestionsForm(), flashes)).ServeHTTP(w, r)
}

func (s *Server) execTemplate(ctx context.Context, conn *websocket.Conn, comp templ.Component) error {
	buf := bytes.NewBuffer(nil)
	comp.Render(ctx, buf)

	return conn.WriteMessage(websocket.TextMessage, buf.Bytes())
}

func (s *Server) writeImage(fname, b64Body string) error {
	fout, err := os.Create(fname)
	if err != nil {
		return err
	}

	imageBytes, err := base64.StdEncoding.DecodeString(b64Body)
	if err != nil {
		os.Remove(fout.Name())
		return err
	}

	fout.Write(imageBytes)
	fout.Close()

	return nil
}

func (s *Server) ImagegenStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	image, err := s.dao.Images().GetByUUID(id)
	if err != nil {
		slog.Error("can't find image", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("failed to upgrade connection", "err", err)
		return
	}
	defer conn.Close()

	tyRef, err := web.Static.ReadFile("static/img/greet.png")
	if err != nil {
		slog.Error("can't open image for ty", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now()
	s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
		Updated: &now,
	}))

	prompt := fmt.Sprintf("Attached is a reference image of the cartoon tiger Ty. Draw Ty in the following scenario:\n\nWhat is there?\n%s\n\nWhat is it like?\n%s\n\nWhat else is there?\n%s\n\nUse a %s style.", image.WhatIsThere, image.WhatIsItLike, image.WhereIsIt, image.Style)

	slog.Info("making image")

	stream := s.oai.Images.EditStreaming(r.Context(), openai.ImageEditParams{
		Image: openai.ImageEditParamsImageUnion{
			OfFile: openai.File(bytes.NewReader(tyRef), "ty-happy.png", "image/png"),
		},
		Prompt:        prompt,
		Model:         openai.ImageModelGPTImage1,
		N:             openai.Int(1),
		User:          openai.String(r.UserAgent()),
		InputFidelity: openai.ImageEditParamsInputFidelityHigh,
		OutputFormat:  openai.ImageEditParamsOutputFormatWebP,
		Quality:       openai.ImageEditParamsQualityHigh,
		Size:          openai.ImageEditParamsSize1536x1024,
	})

	t := time.NewTicker(15 * time.Second)

	go func() {
		for t := range t.C {
			slog.Info("sending update", "image", image.UUID)
			s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
				Updated: &t,
			}))
		}
	}()

	for stream.Next() {
		ev := stream.Current()
		slog.Info("got event", "type", fmt.Sprintf("%T", ev.AsAny()))

		switch variant := ev.AsAny().(type) {
		case openai.ImageEditPartialImageEvent:
			slog.Info("got partial event", "id", image.UUID, "index", variant.PartialImageIndex)

			s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
				Status: fmt.Sprintf("Partial image %d", variant.PartialImageIndex+1),
			}))

			fout, err := os.Create(fmt.Sprintf("./var/%s_%d.webp", image.UUID, variant.PartialImageIndex))
			if err != nil {
				slog.Error("can't create output file", "err", err)
				s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
					Error: err.Error(),
				}))
				return
			}

			imageBytes, err := base64.StdEncoding.DecodeString(variant.B64JSON)
			if err != nil {
				slog.Error("can't decode output file bytes", "err", err)
				s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
					Error: err.Error(),
				}))
				return
			}

			fout.Write(imageBytes)
			fout.Close()
		case openai.ImageEditCompletedEvent:
			t.Stop()
			slog.Info("image done", "id", image.UUID)

			if err := s.writeImage(fmt.Sprintf("./var/%s.webp", image.UUID), variant.B64JSON); err != nil {
				slog.Error("can't decode output file bytes", "err", err)
				s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
					Error: err.Error(),
				}))
				return
			}

			image.Finished = true
			s.dao.Images().Update(image)
			s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
				Status: "Done!",
				Done:   true,
			}))
		default:
			slog.Error("got unknown event type", "type", fmt.Sprintf("%T", ev.AsAny()), "data", ev)
		}
	}

	if err := stream.Err(); err != nil {
		slog.Error("can't read from stream", "err", err)
		s.execTemplate(r.Context(), conn, web.StreamingResponseChunk(web.StreamingChunk{
			Error: err.Error(),
		}))
		return
	}

	// for i, data := range image.Data {
	// 	fout, err := os.Create(fmt.Sprintf("./var/%s_%d.webp", result.ID, i))
	// 	if err != nil {
	// 		slog.Error("can't open output image file for ty", "err", err)
	// 		http.Error(w, err.Error(), http.StatusBadRequest)
	// 		return
	// 	}
	// 	imageBytes, err := base64.StdEncoding.DecodeString(data.B64JSON)
	// 	if err != nil {
	// 		slog.Error("can't decode images for ty", "err", err)
	// 		http.Error(w, err.Error(), http.StatusBadRequest)
	// 		return
	// 	}
	// 	fout.Write(imageBytes)
	// 	result.ImageURLs = append(result.ImageURLs, "data:image/webp;base64,"+base64.RawURLEncoding.EncodeToString(imageBytes))
	// }
}

func (s *Server) Submit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		slog.Error("can't parse form", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := uuid.Must(uuid.NewV7()).String()

	result := models.Image{
		UUID:         id,
		WhatIsThere:  r.FormValue("whatIsThere"),
		WhatIsItLike: r.FormValue("whatLike"),
		WhereIsIt:    r.FormValue("whereIsIt"),
		Style:        r.FormValue("style"),
	}

	if err := s.dao.Images().Create(&result); err != nil {
		slog.Error("can't save image metadata", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	templ.Handler(web.QuestionsResponse(result)).ServeHTTP(w, r)
}

func (s *Server) NotFound(w http.ResponseWriter, r *http.Request) {
	templ.Handler(
		web.Simple("Not found: "+r.URL.Path, web.NotFound(r.URL.Path), nil),
		templ.WithStatus(http.StatusNotFound),
	).ServeHTTP(w, r)
}

// GeneratePresignedURL generates a presigned URL for downloading a file from the reference images bucket
func (s *Server) GeneratePresignedURL(ctx context.Context, bucket, key string) (string, error) {
	presignClient := s3.NewPresignClient(s.s3c)

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    &key,
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(15 * time.Minute)
	})

	if err != nil {
		return "", fmt.Errorf("failed to presign request: %w", err)
	}

	return request.URL, nil
}
