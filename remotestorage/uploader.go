// Copyright 2016 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package remotestorage

import (
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"golang.org/x/net/context"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
)

type Uploader interface {
	UploadFile(bucket, src, dst string, opts ...OpOption) error
	UploadDir(bucket, src, dst string, opts ...OpOption) error
}

// GoogleCloudStorage wraps Google Cloud Storage API.
type GoogleCloudStorage struct {
	JSONKey []byte
	Project string
	Config  *jwt.Config
}

func NewGoogleCloudStorage(key []byte, project string) (Uploader, error) {
	conf, err := google.JWTConfigFromJSON(
		key,
		storage.ScopeFullControl,
	)
	if err != nil {
		return nil, err
	}
	return &GoogleCloudStorage{
		JSONKey: key,
		Project: project,
		Config:  conf,
	}, nil
}

func (g *GoogleCloudStorage) UploadFile(bucket, src, dst string, opts ...OpOption) error {
	if g == nil {
		return fmt.Errorf("GoogleCloudStorage is nil")
	}
	ret := &Op{}
	ret.applyOpts(opts)

	ctx := context.Background()
	admin, err := storage.NewAdminClient(ctx, g.Project, cloud.WithTokenSource(g.Config.TokenSource(ctx)))
	if err != nil {
		return err
	}
	defer admin.Close()

	if err := admin.CreateBucket(context.Background(), bucket, nil); err != nil {
		if !strings.Contains(err.Error(), "You already own this bucket. Please select another name") {
			return err
		}
	}

	sctx := context.Background()
	client, err := storage.NewClient(sctx, cloud.WithTokenSource(g.Config.TokenSource(sctx)))
	if err != nil {
		return err
	}
	defer client.Close()

	wc := client.Bucket(bucket).Object(dst).NewWriter(context.Background())
	if ret.ContentType != "" {
		wc.ContentType = ret.ContentType
	}

	log.Printf("UploadFile: %s ---> %s", src, dst)
	bts, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	if _, err := wc.Write(bts); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	log.Println("UploadFile success")
	return nil
}

func (g *GoogleCloudStorage) UploadDir(bucket, src, dst string, opts ...OpOption) error {
	if g == nil {
		return fmt.Errorf("GoogleCloudStorage is nil")
	}
	ret := &Op{}
	ret.applyOpts(opts)

	ctx := context.Background()
	admin, err := storage.NewAdminClient(ctx, g.Project, cloud.WithTokenSource(g.Config.TokenSource(ctx)))
	if err != nil {
		return err
	}
	defer admin.Close()

	if err := admin.CreateBucket(context.Background(), bucket, nil); err != nil {
		if !strings.Contains(err.Error(), "You already own this bucket. Please select another name") {
			return err
		}
	}

	sctx := context.Background()
	client, err := storage.NewClient(sctx, cloud.WithTokenSource(g.Config.TokenSource(sctx)))
	if err != nil {
		return err
	}
	defer client.Close()

	fmap, err := walkRecursive(src)
	if err != nil {
		return err
	}

	donec, errc := make(chan struct{}), make(chan error)
	for source, destination := range fmap {
		go func(src, dst string) {
			log.Printf("UploadDir: %s ---> %s", src, dst)
			wc := client.Bucket(bucket).Object(dst).NewWriter(context.Background())
			if ret.ContentType != "" {
				wc.ContentType = ret.ContentType
			}
			bts, err := ioutil.ReadFile(src)
			if err != nil {
				errc <- err
				return
			}
			if _, err := wc.Write(bts); err != nil {
				errc <- err
				return
			}
			if err := wc.Close(); err != nil {
				errc <- err
				return
			}
			donec <- struct{}{}
		}(source, destination)
	}

	cnt, num := 0, len(fmap)
	for cnt != num {
		select {
		case <-donec:
		case err := <-errc:
			return err
		}
		cnt++
	}
	log.Println("UploadDir success")
	return nil
}