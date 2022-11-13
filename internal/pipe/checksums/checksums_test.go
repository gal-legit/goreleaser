package checksums

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goreleaser/goreleaser/internal/artifact"
	"github.com/goreleaser/goreleaser/pkg/config"
	"github.com/goreleaser/goreleaser/pkg/context"
	"github.com/stretchr/testify/require"
)

func TestDescription(t *testing.T) {
	require.NotEmpty(t, Pipe{}.String())
}

func TestPipe(t *testing.T) {
	const binary = "binary"
	const archive = binary + ".tar.gz"
	const linuxPackage = binary + ".rpm"
	const checksums = binary + "_bar_checksums.txt"
	const sum = "61d034473102d7dac305902770471fd50f4c5b26f6831a56dd90b5184b3c30fc  "

	tests := map[string]struct {
		ids  []string
		want string
	}{
		"default": {
			want: strings.Join([]string{
				sum + binary,
				sum + linuxPackage,
				sum + archive,
			}, "\n") + "\n",
		},
		"select ids": {
			ids: []string{
				"id-1",
				"id-2",
			},
			want: strings.Join([]string{
				sum + binary,
				sum + archive,
			}, "\n") + "\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			folder := t.TempDir()
			file := filepath.Join(folder, binary)
			require.NoError(t, os.WriteFile(file, []byte("some string"), 0o644))
			ctx := context.New(
				config.Project{
					Dist:        folder,
					ProjectName: binary,
					Checksum: config.Checksum{
						NameTemplate: "{{ .ProjectName }}_{{ .Env.FOO }}_checksums.txt",
						Algorithm:    "sha256",
						IDs:          tt.ids,
					},
				},
			)
			ctx.Git.CurrentTag = "1.2.3"
			ctx.Env = map[string]string{"FOO": "bar"}
			ctx.Artifacts.Add(&artifact.Artifact{
				Name: binary,
				Path: file,
				Type: artifact.UploadableBinary,
				Extra: map[string]interface{}{
					artifact.ExtraID: "id-1",
				},
			})
			ctx.Artifacts.Add(&artifact.Artifact{
				Name: archive,
				Path: file,
				Type: artifact.UploadableArchive,
				Extra: map[string]interface{}{
					artifact.ExtraID: "id-2",
				},
			})
			ctx.Artifacts.Add(&artifact.Artifact{
				Name: linuxPackage,
				Path: file,
				Type: artifact.LinuxPackage,
				Extra: map[string]interface{}{
					artifact.ExtraID: "id-3",
				},
			})
			require.NoError(t, Pipe{}.Run(ctx))
			var artifacts []string
			for _, a := range ctx.Artifacts.List() {
				artifacts = append(artifacts, a.Name)
				require.NoError(t, a.Refresh(), "refresh should not fail and yield same results as nothing changed")
			}
			require.Contains(t, artifacts, checksums, binary)
			bts, err := os.ReadFile(filepath.Join(folder, checksums))
			require.NoError(t, err)
			require.Contains(t, tt.want, string(bts))
		})
	}
}

func TestRefreshModifying(t *testing.T) {
	const binary = "binary"
	folder := t.TempDir()
	file := filepath.Join(folder, binary)
	require.NoError(t, os.WriteFile(file, []byte("some string"), 0o644))
	ctx := context.New(
		config.Project{
			Dist:        folder,
			ProjectName: binary,
			Checksum: config.Checksum{
				NameTemplate: "{{ .ProjectName }}_{{ .Env.FOO }}_checksums.txt",
				Algorithm:    "sha256",
			},
		},
	)
	ctx.Git.CurrentTag = "1.2.3"
	ctx.Env = map[string]string{"FOO": "bar"}
	ctx.Artifacts.Add(&artifact.Artifact{
		Name: binary,
		Path: file,
		Type: artifact.UploadableBinary,
	})
	require.NoError(t, Pipe{}.Run(ctx))
	checks := ctx.Artifacts.Filter(artifact.ByType(artifact.Checksum)).List()
	require.Len(t, checks, 1)
	previous, err := os.ReadFile(checks[0].Path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte("some other string"), 0o644))
	require.NoError(t, checks[0].Refresh())
	current, err := os.ReadFile(checks[0].Path)
	require.NoError(t, err)
	require.NotEqual(t, string(previous), string(current))
}

func TestPipeFileNotExist(t *testing.T) {
	folder := t.TempDir()
	ctx := context.New(
		config.Project{
			Dist: folder,
			Checksum: config.Checksum{
				NameTemplate: "checksums.txt",
			},
		},
	)
	ctx.Git.CurrentTag = "1.2.3"
	ctx.Artifacts.Add(&artifact.Artifact{
		Name: "nope",
		Path: "/nope",
		Type: artifact.UploadableBinary,
	})
	err := Pipe{}.Run(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "/nope: no such file or directory")
}

func TestPipeInvalidNameTemplate(t *testing.T) {
	binFile, err := os.CreateTemp(t.TempDir(), "goreleasertest-bin")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, binFile.Close()) })
	_, err = binFile.WriteString("fake artifact")
	require.NoError(t, err)

	for template, eerr := range map[string]string{
		"{{ .Pro }_checksums.txt": `template: tmpl:1: unexpected "}" in operand`,
		"{{.Env.NOPE}}":           `template: tmpl:1:6: executing "tmpl" at <.Env.NOPE>: map has no entry for key "NOPE"`,
	} {
		t.Run(template, func(t *testing.T) {
			folder := t.TempDir()
			ctx := context.New(
				config.Project{
					Dist:        folder,
					ProjectName: "name",
					Checksum: config.Checksum{
						NameTemplate: template,
						Algorithm:    "sha256",
					},
				},
			)
			ctx.Git.CurrentTag = "1.2.3"
			ctx.Artifacts.Add(&artifact.Artifact{
				Name: "whatever",
				Type: artifact.UploadableBinary,
				Path: binFile.Name(),
			})
			err = Pipe{}.Run(ctx)
			require.Error(t, err)
			require.Equal(t, eerr, err.Error())
		})
	}
}

func TestPipeCouldNotOpenChecksumsTxt(t *testing.T) {
	folder := t.TempDir()
	binFile, err := os.CreateTemp(folder, "goreleasertest-bin")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, binFile.Close()) })
	_, err = binFile.WriteString("fake artifact")
	require.NoError(t, err)

	file := filepath.Join(folder, "checksums.txt")
	require.NoError(t, os.WriteFile(file, []byte("some string"), 0o000))
	ctx := context.New(
		config.Project{
			Dist: folder,
			Checksum: config.Checksum{
				NameTemplate: "checksums.txt",
				Algorithm:    "sha256",
			},
		},
	)
	ctx.Git.CurrentTag = "1.2.3"
	ctx.Artifacts.Add(&artifact.Artifact{
		Name: "whatever",
		Type: artifact.UploadableBinary,
		Path: binFile.Name(),
	})
	err = Pipe{}.Run(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "/checksums.txt: permission denied")
}

func TestPipeWhenNoArtifacts(t *testing.T) {
	ctx := &context.Context{
		Artifacts: artifact.New(),
	}
	require.NoError(t, Pipe{}.Run(ctx))
	require.Len(t, ctx.Artifacts.List(), 0)
}

func TestDefault(t *testing.T) {
	ctx := &context.Context{
		Config: config.Project{
			Checksum: config.Checksum{},
		},
	}
	require.NoError(t, Pipe{}.Default(ctx))
	require.Equal(
		t,
		"{{ .ProjectName }}_{{ .Version }}_checksums.txt",
		ctx.Config.Checksum.NameTemplate,
	)
	require.Equal(t, "sha256", ctx.Config.Checksum.Algorithm)
}

func TestDefaultSet(t *testing.T) {
	ctx := &context.Context{
		Config: config.Project{
			Checksum: config.Checksum{
				NameTemplate: "checksums.txt",
			},
		},
	}
	require.NoError(t, Pipe{}.Default(ctx))
	require.Equal(t, "checksums.txt", ctx.Config.Checksum.NameTemplate)
}

func TestPipeCheckSumsWithExtraFiles(t *testing.T) {
	const binary = "binary"
	const checksums = "checksums.txt"
	const extraFileFooRelPath = "./testdata/foo.txt"
	const extraFileBarRelPath = "./testdata/**/bar.txt"
	const extraFileFoo = "foo.txt"
	const extraFileBar = "bar.txt"

	tests := map[string]struct {
		extraFiles []config.ExtraFile
		ids        []string
		want       []string
	}{
		"default": {
			extraFiles: nil,
			want: []string{
				binary,
			},
		},
		"default_plus_extra": {
			extraFiles: []config.ExtraFile{
				{Glob: extraFileFooRelPath},
			},
			want: []string{
				binary,
				extraFileFoo,
			},
		},
		"one extra file": {
			extraFiles: []config.ExtraFile{
				{Glob: extraFileFooRelPath},
			},
			want: []string{
				extraFileFoo,
			},
		},
		"multiple extra files": {
			extraFiles: []config.ExtraFile{
				{Glob: extraFileFooRelPath},
				{Glob: extraFileBarRelPath},
			},
			want: []string{
				extraFileFoo,
				extraFileBar,
			},
		},
		"one extra file with no builds": {
			extraFiles: []config.ExtraFile{
				{Glob: extraFileFooRelPath},
			},
			ids: []string{"yada yada yada"},
			want: []string{
				extraFileFoo,
			},
		},
	}
	checksumsMap := map[string]string{
		"crc32":  "f94d3859",
		"md5":    "5ac749fbeec93607fc28d666be85e73a",
		"sha1":   "8b45e4bd1c6acb88bebf6407d16205f567e62a3e",
		"sha224": "21bc225587d8768058837b68fe7e0341e87b972f02fd8fb0c236d1d3",
		"sha256": "61d034473102d7dac305902770471fd50f4c5b26f6831a56dd90b5184b3c30fc",
		"sha384": "f6055a96a105d2fb5941a616964ffda8294fd415730cc4154a602062bc3d00e99d3c6f4a11af8c965a343de4afca3c2b",
		"sha512": "14925e01a7a0cf0801aa95fe52d542b578af58ae7997ada66db3a6eae68a329d50600a5b7b442eabf4ea77ea8ef5fe40acf2ab31d47311b2a232c4f64009aac1",
	}

	for algo, value := range checksumsMap {
		algoTestCounter := 0
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				folder := t.TempDir()
				file := filepath.Join(folder, binary)
				require.NoError(t, os.WriteFile(file, []byte("some string"), 0o644))
				ctx := context.New(
					config.Project{
						Dist:        folder,
						ProjectName: binary,
						Checksum: config.Checksum{
							Algorithm:    algo,
							NameTemplate: "checksums.txt",
							ExtraFiles:   tt.extraFiles,
							IDs:          tt.ids,
						},
					},
				)

				ctx.Artifacts.Add(&artifact.Artifact{
					Name: binary,
					Path: file,
					Type: artifact.UploadableBinary,
					Extra: map[string]interface{}{
						artifact.ExtraID: "id-1",
					},
				})

				require.NoError(t, Pipe{}.Run(ctx))

				bts, err := os.ReadFile(filepath.Join(folder, checksums))

				require.NoError(t, err)
				if algo == "sha256" {
					for _, want := range tt.want {
						if want == binary {
							require.Contains(t, string(bts), "61d034473102d7dac305902770471fd50f4c5b26f6831a56dd90b5184b3c30fc  "+want)
						} else if want == extraFileFoo {
							require.Contains(t, string(bts), "3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  "+want)
						}
					}
				}

				wantBinary := false
				for _, want := range tt.want {
					if want == binary {
						wantBinary = true
						break
					}
				}
				if wantBinary {
					_ = ctx.Artifacts.Filter(artifact.ByType(artifact.UploadableBinary)).Visit(func(a *artifact.Artifact) error {
						if a.Path != file {
							return nil
						}

						algoTestCounter += 1
						checksum, ok := a.Extra[artifactChecksumExtra]
						require.True(t, ok)

						expectedChecksum := fmt.Sprintf("%s:%s", algo, value)
						require.Equal(t, expectedChecksum, checksum)

						return nil
					})
				}
			})
		}
		const tests_that_include_binary = 2
		require.Equal(t, tests_that_include_binary, algoTestCounter)
	}
}

func TestExtraFilesNoMatch(t *testing.T) {
	dir := t.TempDir()
	ctx := context.New(
		config.Project{
			Dist:        dir,
			ProjectName: "fake",
			Checksum: config.Checksum{
				Algorithm:    "sha256",
				NameTemplate: "checksums.txt",
				ExtraFiles:   []config.ExtraFile{{Glob: "./nope/nope.txt"}},
			},
		},
	)

	ctx.Artifacts.Add(&artifact.Artifact{
		Name: "fake",
		Path: "fake-path",
		Type: artifact.UploadableBinary,
	})

	require.NoError(t, Pipe{}.Default(ctx))
	require.EqualError(t, Pipe{}.Run(ctx), `globbing failed for pattern ./nope/nope.txt: matching "./nope/nope.txt": file does not exist`)
}

func TestSkip(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		ctx := context.New(config.Project{
			Checksum: config.Checksum{
				Disable: true,
			},
		})
		require.True(t, Pipe{}.Skip(ctx))
	})

	t.Run("dont skip", func(t *testing.T) {
		require.False(t, Pipe{}.Skip(context.New(config.Project{})))
	})
}

// TODO: add tests for LinuxPackage and UploadableSourceArchive
