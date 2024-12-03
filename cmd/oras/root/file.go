/*
Copyright The ORAS Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package root

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras/cmd/oras/internal/display/status"

	"github.com/agoda-com/macOS-vz-kubelet/pkg/oci"
	"github.com/virtual-kubelet/virtual-kubelet/log"
)

func loadFiles(ctx context.Context, store *oci.Store, annotations map[string]map[string]string, mediaTypeToFilename map[string]string, displayStatus status.PushHandler) ([]ocispec.Descriptor, error) {
	var files []ocispec.Descriptor
	for mediaType, filename := range mediaTypeToFilename {
		// get shortest absolute path as unique name
		name := filepath.Clean(filename)
		if !filepath.IsAbs(name) {
			name = filepath.ToSlash(name)
		}

		err := displayStatus.OnFileLoading(name)
		if err != nil {
			return nil, err
		}

		log.G(ctx).Infof("packaging file %s:%s", mediaType, name)
		file, err := addFile(ctx, store, mediaType, filename)
		if err != nil {
			return nil, err
		}

		if value, ok := annotations[filename]; ok {
			if file.Annotations == nil {
				file.Annotations = value
			} else {
				for k, v := range value {
					file.Annotations[k] = v
				}
			}
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		if err := displayStatus.OnEmptyArtifact(); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func addFile(ctx context.Context, store *oci.Store, mediaType string, filename string) (ocispec.Descriptor, error) {
	file, err := store.Add(ctx, mediaType, filename)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr
		}
		return ocispec.Descriptor{}, err
	}
	return file, nil
}
