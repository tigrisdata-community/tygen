package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/rbcervilla/redisstore/v9"
	"github.com/tigrisdata-community/tygen/models"
	"github.com/tigrisdata-community/tygen/web"
)

const sessionName = "session"

type Options struct {
	DatabaseURL           string
	RedisURL              string
	S3Client              *s3.Client
	ReferenceImagesBucket string
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
		referenceImagesBucket: opts.ReferenceImagesBucket,
	}

	return result, nil
}

type Server struct {
	dao                   *models.DAO
	store                 *redisstore.RedisStore
	s3c                   *s3.Client
	referenceImagesBucket string
}

func (s *Server) register(mux *http.ServeMux) {
	web.Mount(mux)
	mux.HandleFunc("/{$}", s.Index)
	mux.HandleFunc("/", s.NotFound)
	mux.HandleFunc("POST /submit", s.Submit)
}

func (s *Server) Index(w http.ResponseWriter, r *http.Request) {
	flashes, _ := getFlashes(r.Context())

	templ.Handler(web.Simple("Tygen", web.QuestionsForm(), flashes)).ServeHTTP(w, r)
}

func (s *Server) Submit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		slog.Error("can't parse form", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := uuid.Must(uuid.NewV7()).String()

	result := web.FormResult{
		ID:          id,
		WhatIsThere: r.FormValue("whatIsThere"),
		WhatLike:    r.FormValue("whatLike"),
		WhereIsIt:   r.FormValue("whereIsIt"),
		Style:       r.FormValue("style"),
	}

	templ.Handler(web.QuestionsResponse(result)).ServeHTTP(w, r)
}

func (s *Server) AddFlash(w http.ResponseWriter, r *http.Request) {
	session, _ := getSession(r.Context())

	var flash models.Flash

	switch r.PathValue("kind") {
	case string(models.FlashInfo):
		flash = models.NewFlash(models.FlashInfo, "<p>Welcome!</p>")
	case string(models.FlashFailure):
		flash = models.NewFlash(models.FlashFailure, "<p>Oh no, something went wrong!</p>")
	case string(models.FlashSuccess):
		flash = models.NewFlash(models.FlashSuccess, "<p>Oh yes, something went right!</p>")
	case string(models.FlashWarning):
		flash = models.NewFlash(models.FlashWarning, "<p>Are you sure you wanted to do that?</p>")
	default:
		templ.Handler(
			web.Simple("Not found: "+r.URL.Path, web.NotFound(r.URL.Path), nil),
			templ.WithStatus(http.StatusNotFound),
		).ServeHTTP(w, r)
		return
	}

	session.AddFlash(flash)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) NotFound(w http.ResponseWriter, r *http.Request) {
	templ.Handler(
		web.Simple("Not found: "+r.URL.Path, web.NotFound(r.URL.Path), nil),
		templ.WithStatus(http.StatusNotFound),
	).ServeHTTP(w, r)
}

// GeneratePresignedURL generates a presigned URL for downloading a file from the reference images bucket
func (s *Server) GeneratePresignedURL(ctx context.Context, key string) (string, error) {
	presignClient := s3.NewPresignClient(s.s3c)

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.referenceImagesBucket,
		Key:    &key,
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(15 * time.Minute)
	})

	if err != nil {
		return "", fmt.Errorf("failed to presign request: %w", err)
	}

	return request.URL, nil
}
