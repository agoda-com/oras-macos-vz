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
	"fmt"
	"os"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras/cmd/oras/internal/argument"
	"oras.land/oras/cmd/oras/internal/command"
	"oras.land/oras/cmd/oras/internal/display"
	"oras.land/oras/cmd/oras/internal/display/status"
	oerrors "oras.land/oras/cmd/oras/internal/errors"
	"oras.land/oras/cmd/oras/internal/option"
	"oras.land/oras/internal/contentutil"
	"oras.land/oras/internal/listener"
	"oras.land/oras/internal/registryutil"

	"github.com/agoda-com/macOS-vz-kubelet/pkg/event"
	"github.com/agoda-com/macOS-vz-kubelet/pkg/oci"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	logruslogger "github.com/virtual-kubelet/virtual-kubelet/log/logrus"
)

const (
	artifactType = "application/vnd.agoda.macosvz.artifact"
)

type pushOptions struct {
	option.Common
	option.Packer
	// option.ImageSpec
	option.Target
	option.Format

	diskPath      string
	auxPath       string
	hardwareModel string
	machineID     string

	extraRefs   []string
	concurrency int
}

func pushCmd() *cobra.Command {
	var opts pushOptions
	cmd := &cobra.Command{
		Use:   "push --disk-path <path> --aux-path <path> --hardware-model <base64_string> --machine-id <base64_string> [flags] <name>[:<tag>[,<tag>][...]] <file>[:<type>] [...]",
		Short: "Package macOS virtual machine image using the macOS-vz-kubelet format and push it to an OCI registry",
		Long: `Package macOS virtual machine image using the macOS-vz-kubelet format and push it to an OCI registry

Example - Package and push a virtual machine image to the OCI registry using the macOS-vz-kubelet format (default):
  oras push --disk-path /path/to/disk.img --aux-path /path/to/aux.img --hardware-model "e30=" --machine-id "e30=" localhost:5000/my-vm-image:latest

Example - Package and push a virtual machine image to the OCI registry using the macOS-vz-kubelet format and export the pushed manifest to a specified path:
  oras push --disk-path /path/to/disk.img --aux-path /path/to/aux.img --hardware-model "e30=" --machine-id "e30=" --export-manifest manifest.json localhost:5000/my-vm-image:latest

Example - Package and push a virtual machine image to the insecure registry using the macOS-vz-kubelet format:
  oras push --disk-path /path/to/disk.img --aux-path /path/to/aux.img --hardware-model "e30=" --machine-id "e30=" --insecure localhost:5000/my-vm-image:latest

Example - Package and push a virtual machine image to the HTTP registry using the macOS-vz-kubelet format:
  oras push --disk-path /path/to/disk.img --aux-path /path/to/aux.img --hardware-model "e30=" --machine-id "e30=" --plain-http localhost:5000/my-vm-image:latest

Example - Package and push a virtual machine image to the OCI registry using the macOS-vz-kubelet format with multiple tags:
  oras push --disk-path /path/to/disk.img --aux-path /path/to/aux.img --hardware-model "e30=" --machine-id "e30=" localhost:5000/my-vm-image:tag1,tag2,tag3

Example - Package and push a virtual machine image to the OCI registry using the macOS-vz-kubelet format with multiple tags and concurrency level tuned:
  oras push --disk-path /path/to/disk.img --aux-path /path/to/aux.img --hardware-model "e30=" --machine-id "e30=" --concurrency 6 localhost:5000/my-vm-image:tag1,tag2,tag3
`,
		Args: oerrors.CheckArgs(argument.AtLeast(1), "the destination for pushing"),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			refs := strings.Split(args[0], ",")
			opts.RawReference = refs[0]
			opts.extraRefs = refs[1:]
			if err := option.Parse(cmd, &opts); err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd, &opts)
		},
	}

	cmd.Flags().StringVar(&opts.diskPath, "disk-path", "", "Path to the virtual machine disk image")
	cmd.Flags().StringVar(&opts.auxPath, "aux-path", "", "Path to the auxiliary image")
	cmd.Flags().StringVar(&opts.hardwareModel, "hardware-model", "", "Hardware model identifier")
	cmd.Flags().StringVar(&opts.machineID, "machine-id", "", "Machine ID")

	cmd.Flags().IntVarP(&opts.concurrency, "concurrency", "", 5, "concurrency level")
	opts.SetTypes(option.FormatTypeText, option.FormatTypeJSON, option.FormatTypeGoTemplate)
	option.ApplyFlags(&opts, cmd.Flags())
	return oerrors.Command(cmd, &opts.Target)
}

func validateOptions(opts *pushOptions) error {
	if opts.diskPath == "" || opts.auxPath == "" || opts.hardwareModel == "" || opts.machineID == "" {
		return fmt.Errorf("must provide disk-path, aux-path, hardware-model, and machine-id")
	}
	return nil
}

func runPush(cmd *cobra.Command, opts *pushOptions) error {
	if err := validateOptions(opts); err != nil {
		return err
	}

	ctx, logger := command.GetLogger(cmd, &opts.Common)
	log.L = logruslogger.FromLogrus(logger.(*logrus.Entry))
	ctx = log.WithLogger(ctx, log.L)

	displayStatus, displayMetadata, err := display.NewPushHandler(cmd.OutOrStdout(), opts.Format, opts.TTY, opts.Verbose)
	if err != nil {
		return err
	}
	annotations, err := opts.LoadManifestAnnotations()
	if err != nil {
		return err
	}

	// prepare pack
	config := oci.NewMacOSConfig(opts.hardwareModel, opts.machineID)
	store, err := oci.New(os.TempDir(), true, event.LogEventRecorder{})
	if err != nil {
		return err
	}
	defer store.Close(ctx)

	layers, err := loadFiles(
		ctx,
		store,
		annotations,
		map[string]string{
			string(oci.MediaTypeDiskImage): opts.diskPath,
			string(oci.MediaTypeAuxImage):  opts.auxPath,
		},
		displayStatus,
	)
	if err != nil {
		return err
	}

	configDesc, err := store.Set(ctx, config)
	if err != nil {
		return err
	}
	layers = append(layers, configDesc)

	manifestConfigDesc, err := store.GetManifestConfigDescriptor(ctx)
	if err != nil {
		return err
	}
	packOpts := oras.PackManifestOptions{
		Layers:           layers,
		ConfigDescriptor: &manifestConfigDesc,
	}

	memoryStore := memory.New()
	pack := func() (ocispec.Descriptor, error) {
		root, err := oras.PackManifest(ctx, memoryStore, oras.PackManifestVersion1_1, artifactType, packOpts)
		if err != nil {
			return ocispec.Descriptor{}, err
		}
		if err = memoryStore.Tag(ctx, root, root.Digest.String()); err != nil {
			return ocispec.Descriptor{}, err
		}
		return root, nil
	}

	// prepare push
	originalDst, err := opts.NewTarget(opts.Common, logger)
	if err != nil {
		return err
	}
	dst, stopTrack, err := displayStatus.TrackTarget(originalDst)
	if err != nil {
		return err
	}
	copyOptions := oras.DefaultCopyOptions
	copyOptions.Concurrency = opts.concurrency
	union := contentutil.MultiReadOnlyTarget(memoryStore, store)
	displayStatus.UpdateCopyOptions(&copyOptions.CopyGraphOptions, union)
	copy := func(root ocispec.Descriptor) error {
		// add both pull and push scope hints for dst repository
		// to save potential push-scope token requests during copy
		ctx = registryutil.WithScopeHint(ctx, dst, auth.ActionPull, auth.ActionPush)

		if tag := opts.Reference; tag == "" {
			err = oras.CopyGraph(ctx, union, dst, root, copyOptions.CopyGraphOptions)
		} else {
			_, err = oras.Copy(ctx, union, root.Digest.String(), dst, tag, copyOptions)
		}
		return err
	}

	// Push
	root, err := doPush(dst, stopTrack, pack, copy)
	if err != nil {
		return err
	}
	err = displayMetadata.OnCopied(&opts.Target)
	if err != nil {
		return err
	}

	if len(opts.extraRefs) != 0 {
		contentBytes, err := content.FetchAll(ctx, memoryStore, root)
		if err != nil {
			return err
		}
		tagBytesNOpts := oras.DefaultTagBytesNOptions
		tagBytesNOpts.Concurrency = opts.concurrency
		dst := listener.NewTagListener(originalDst, nil, displayMetadata.OnTagged)
		if _, err = oras.TagBytesN(ctx, dst, root.MediaType, contentBytes, opts.extraRefs, tagBytesNOpts); err != nil {
			return err
		}
	}

	err = displayMetadata.OnCompleted(root)
	if err != nil {
		return err
	}

	// Export manifest
	return opts.ExportManifest(ctx, memoryStore, root)
}

func doPush(dst oras.Target, stopTrack status.StopTrackTargetFunc, pack packFunc, copy copyFunc) (ocispec.Descriptor, error) {
	defer func() {
		_ = stopTrack()
	}()
	// Push
	return pushArtifact(dst, pack, copy)
}

type packFunc func() (ocispec.Descriptor, error)
type copyFunc func(desc ocispec.Descriptor) error

func pushArtifact(_ oras.Target, pack packFunc, copy copyFunc) (ocispec.Descriptor, error) {
	root, err := pack()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	// push
	if err = copy(root); err != nil {
		return ocispec.Descriptor{}, err
	}
	return root, nil
}
