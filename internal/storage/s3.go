package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"slimserve/internal/config"
	"slimserve/internal/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/cespare/xxhash/v2"
)

type S3Backend struct {
	client         *s3.Client
	bucket         string
	prefix         string
	cache          *ByteCache
	cfg            *config.DirectoryConfig
	ignorePatterns []string
	inFlight       sync.Map
}

type S3Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	IsDir        bool
}

func NewS3Backend(cfg *config.DirectoryConfig, cacheMaxBytes int64, ignorePatterns []string) (*S3Backend, error) {
	var opts []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		creds := aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     cfg.AccessKey,
				SecretAccessKey: cfg.SecretKey,
			}, nil
		})
		opts = append(opts, awsconfig.WithCredentialsProvider(creds))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	var cache *ByteCache
	if cacheMaxBytes > 0 {
		cache = NewByteCache(cacheMaxBytes)
	}

	return &S3Backend{
		client:         client,
		bucket:         cfg.Path,
		prefix:         cfg.Prefix,
		cache:          cache,
		cfg:            cfg,
		ignorePatterns: ignorePatterns,
	}, nil
}

func (s *S3Backend) Path() string {
	if s.prefix == "" {
		return "s3://" + s.bucket
	}
	return "s3://" + s.bucket + "/" + s.prefix
}

func (s *S3Backend) fullPath(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func (s *S3Backend) cacheKey(key string) string {
	fullKey := s.bucket + "/" + s.fullPath(key)
	h := xxhash.Sum64String(fullKey)
	return fmt.Sprintf("%016x", h)
}

func (s *S3Backend) StatObject(ctx context.Context, key string) (*S3Object, error) {
	fullKey := s.fullPath(key)

	resp, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		var notFound *types.NotFound
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &notFound) || errors.As(err, &noSuchKey) {
			return nil, nil
		}
		return nil, fmt.Errorf("head object: %w", err)
	}

	return &S3Object{
		Key:          key,
		Size:         *resp.ContentLength,
		LastModified: *resp.LastModified,
		IsDir:        false,
	}, nil
}

func (s *S3Backend) Get(ctx context.Context, key string) ([]byte, error) {
	cacheKey := s.cacheKey(key)

	if s.cache != nil {
		if data, ok := s.cache.Get(cacheKey); ok {
			logger.Log.Debug().Str("key", key).Msg("S3 cache hit")
			return data, nil
		}
	}

	type flightResult struct {
		data []byte
		err  error
	}

	load := func() ([]byte, error) {
		fullKey := s.fullPath(key)
		resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(fullKey),
		})
		if err != nil {
			return nil, fmt.Errorf("get object: %w", err)
		}
		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}

		if s.cache != nil {
			s.cache.Set(cacheKey, data)
			logger.Log.Debug().Str("key", key).Int("size", len(data)).Msg("S3 cached")
		}

		return data, nil
	}

	waiterChan := make(chan flightResult, 1)

	actual, loaded := s.inFlight.LoadOrStore(key, waiterChan)
	if loaded {
		waiter := actual.(chan flightResult)
		result := <-waiter
		if result.err != nil {
			return nil, result.err
		}
		if s.cache != nil {
			if data, ok := s.cache.Get(cacheKey); ok {
				return data, nil
			}
		}
		return result.data, nil
	}

	data, err := load()
	waiterChan <- flightResult{data: data, err: err}
	s.inFlight.Delete(key)

	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *S3Backend) Put(ctx context.Context, key string, data []byte) error {
	fullKey := s.fullPath(key)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fullKey),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}

	if s.cache != nil {
		s.cache.Delete(s.cacheKey(key))
	}

	return nil
}

func (s *S3Backend) Delete(ctx context.Context, key string) error {
	fullKey := s.fullPath(key)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}

	if s.cache != nil {
		s.cache.Delete(s.cacheKey(key))
	}

	return nil
}

func (s *S3Backend) List(ctx context.Context, prefix string) ([]S3Object, error) {
	fullPrefix := s.fullPath(prefix)
	if fullPrefix != "" && !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}

	var objects []S3Object
	var continuationToken *string

	for {
		resp, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.bucket),
			Prefix:            aws.String(fullPrefix),
			Delimiter:         aws.String("/"),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}

		for _, obj := range resp.Contents {
			key := *obj.Key
			if s.prefix != "" {
				key = strings.TrimPrefix(key, s.prefix+"/")
			}
			size := int64(0)
			if obj.Size != nil {
				size = *obj.Size
			}
			objects = append(objects, S3Object{
				Key:          key,
				Size:         size,
				LastModified: *obj.LastModified,
				IsDir:        false,
			})
		}

		for _, pfx := range resp.CommonPrefixes {
			name := *pfx.Prefix
			if s.prefix != "" {
				name = strings.TrimPrefix(name, s.prefix+"/")
			}
			objects = append(objects, S3Object{
				Key:   strings.TrimSuffix(name, "/"),
				IsDir: true,
			})
		}

		if resp.IsTruncated == nil || !*resp.IsTruncated {
			break
		}
		continuationToken = resp.NextContinuationToken
	}

	return objects, nil
}

func (s *S3Backend) IsIgnored(ctx context.Context, relPath string) (bool, error) {
	return MatchIgnore(relPath, s.ignorePatterns), nil
}

func (s *S3Backend) Close() error {
	return nil
}

func (s *S3Backend) Move(ctx context.Context, srcKey, destKey string) error {
	srcFullKey := s.fullPath(srcKey)
	destFullKey := s.fullPath(destKey)

	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(s.bucket),
		CopySource: aws.String(s.bucket + "/" + srcFullKey),
		Key:        aws.String(destFullKey),
	})
	if err != nil {
		return fmt.Errorf("copy object: %w", err)
	}

	err = s.Delete(ctx, srcKey)
	if err != nil {
		s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(destFullKey),
		})
		return fmt.Errorf("delete source after copy: %w", err)
	}

	return nil
}
