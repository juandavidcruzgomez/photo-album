// Package settings loads the application configuration from the environment,
// optionally seeded from a .env file.
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// envFiles are tried in order; the first one found is loaded. The .env lives
// at the repository root, one level above the Go module.
var envFiles = []string{".env", "../.env"}

// Settings holds the application configuration.
type Settings struct {
	// DatabaseURL, when set, is used as is and overrides the Postgres* fields.
	DatabaseURL string

	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	PostgresHost     string
	PostgresPort     string

	// SeaweedFS (S3 API). Empty keys mean anonymous access.
	S3Endpoint  string
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	// StoreOriginals makes the indexer copy original files to S3, not just
	// thumbnails.
	StoreOriginals bool

	// CLIP model files used by the indexer.
	OnnxLib   string
	ModelsDir string
}

// Load reads the configuration. Values already in the environment win over
// the .env file, and a missing .env file is not an error (in containers the
// variables are usually set directly).
func Load() (*Settings, error) {
	if err := loadEnvFile(); err != nil {
		return nil, err
	}
	storeOriginals, err := strconv.ParseBool(getenv("STORE_ORIGINALS", "false"))
	if err != nil {
		return nil, fmt.Errorf("STORE_ORIGINALS: %w", err)
	}
	return &Settings{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		PostgresUser:     os.Getenv("POSTGRES_USER"),
		PostgresPassword: os.Getenv("POSTGRES_PASSWORD"),
		PostgresDB:       getenv("POSTGRES_DB", "postgres"),
		PostgresHost:     getenv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getenv("POSTGRES_PORT", "5432"),
		S3Endpoint:       getenv("S3_ENDPOINT", "http://localhost:8333"),
		S3Region:         getenv("S3_REGION", "us-east-1"),
		S3Bucket:         getenv("S3_BUCKET", "photos"),
		S3AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:      os.Getenv("S3_SECRET_KEY"),
		StoreOriginals:   storeOriginals,
		OnnxLib:          getenv("ONNX_LIB", "models/libonnxruntime.so.1.31.0"),
		ModelsDir:        getenv("CLIP_MODELS_DIR", "models/clip-vit-base-patch32"),
	}, nil
}

// DSN returns the Postgres connection string.
func (s *Settings) DSN() string {
	if s.DatabaseURL != "" {
		return s.DatabaseURL
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(s.PostgresUser, s.PostgresPassword),
		Host:     s.PostgresHost + ":" + s.PostgresPort,
		Path:     s.PostgresDB,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

func loadEnvFile() error {
	for _, path := range envFiles {
		err := godotenv.Load(path)
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			if err != nil {
				return fmt.Errorf("loading %s: %w", path, err)
			}
			return nil
		}
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
