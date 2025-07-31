package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net/http"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/facebookgo/flagenv"
	_ "github.com/joho/godotenv/autoload"
	"github.com/tigrisdata-community/tygen/internal"
)

var (
	bind                  = flag.String("bind", ":4000", "TCP host:port to bind on")
	databaseURL           = flag.String("database-url", "", "Postgres database URL")
	redisURL              = flag.String("redis-url", "", "Valkey/Redis database URL")
	slogLevel             = flag.String("slog-level", "INFO", "log level")
	referenceImagesBucket = flag.String("reference-images-bucket", "xe-ty-reference-images", "Bucket full of reference images")
	tigrisBucket          = flag.String("tigris-bucket", "xe-tygen-dev", "Tigris bucket to push generated images to")
)

func main() {
	flagenv.Parse()
	flag.Parse()

	internal.InitSlog(*slogLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := awsConfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}

	s3c := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = false
	})

	s, err := New(Options{
		DatabaseURL:           *databaseURL,
		RedisURL:              *redisURL,
		S3Client:              s3c,
		ReferenceImagesBucket: *referenceImagesBucket,
		TigrisBucket:          *tigrisBucket,
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	s.register(mux)

	var h http.Handler = mux
	h = s.sessionMiddleware(h)

	slog.Info("now listening", "url", "http://localhost"+*bind)
	log.Fatal(http.ListenAndServe(*bind, h))
}
