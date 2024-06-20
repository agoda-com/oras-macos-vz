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
	"github.com/spf13/cobra"
	"oras.land/oras/cmd/oras/root/blob"
	"oras.land/oras/cmd/oras/root/manifest"
	"oras.land/oras/cmd/oras/root/repo"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "oras [command]",
		SilenceUsage: true,
	}
	cmd.AddCommand(
		pullCmd(),
		pushCmd(),
		loginCmd(),
		logoutCmd(),
		versionCmd(),
		discoverCmd(),
		resolveCmd(),
		copyCmd(),
		tagCmd(),
		// attachCmd(), // not supported for macos-vz format
		blob.Cmd(),
		manifest.Cmd(),
		repo.Cmd(),
	)
	return cmd
}
