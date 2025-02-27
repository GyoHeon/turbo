package hashing

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/vercel/turbo/cli/internal/fs"
	"github.com/vercel/turbo/cli/internal/turbopath"
	"gotest.tools/v3/assert"
)

func getFixture(id int) turbopath.AbsoluteSystemPath {
	cwd, _ := os.Getwd()
	root := turbopath.AbsoluteSystemPath(filepath.VolumeName(cwd) + string(os.PathSeparator))
	checking := turbopath.AbsoluteSystemPath(cwd)

	for checking != root {
		fixtureDirectory := checking.Join("fixtures")
		_, err := os.Stat(fixtureDirectory.ToString())
		if !errors.Is(err, os.ErrNotExist) {
			// Found the fixture directory!
			files, _ := os.ReadDir(fixtureDirectory.ToString())

			// Grab the specified fixture.
			for _, file := range files {
				fileName := turbopath.RelativeSystemPath(file.Name())
				if strings.Index(fileName.ToString(), fmt.Sprintf("%02d-", id)) == 0 {
					return turbopath.AbsoluteSystemPath(fixtureDirectory.Join(fileName))
				}
			}
		}
		checking = checking.Join("..")
	}

	panic("fixtures not found!")
}

func TestSpecialCharacters(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}

	fixturePath := getFixture(1)
	newlinePath := turbopath.AnchoredUnixPath("new\nline").ToSystemPath()
	quotePath := turbopath.AnchoredUnixPath("\"quote\"").ToSystemPath()
	newline := newlinePath.RestoreAnchor(fixturePath)
	quote := quotePath.RestoreAnchor(fixturePath)

	// Setup
	one := os.WriteFile(newline.ToString(), []byte{}, 0644)
	two := os.WriteFile(quote.ToString(), []byte{}, 0644)

	// Cleanup
	defer func() {
		one := os.Remove(newline.ToString())
		two := os.Remove(quote.ToString())

		if one != nil || two != nil {
			return
		}
	}()

	// Setup error check
	if one != nil || two != nil {
		return
	}

	tests := []struct {
		name        string
		rootPath    turbopath.AbsoluteSystemPath
		filesToHash []turbopath.AnchoredSystemPath
		want        map[turbopath.AnchoredUnixPath]string
		wantErr     bool
	}{
		{
			name:     "Quotes",
			rootPath: fixturePath,
			filesToHash: []turbopath.AnchoredSystemPath{
				quotePath,
			},
			want: map[turbopath.AnchoredUnixPath]string{
				quotePath.ToUnixPath(): "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
			},
		},
		{
			name:     "Newlines",
			rootPath: fixturePath,
			filesToHash: []turbopath.AnchoredSystemPath{
				newlinePath,
			},
			want: map[turbopath.AnchoredUnixPath]string{
				newlinePath.ToUnixPath(): "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetHashesForFiles(tt.rootPath, tt.filesToHash)
			if (err != nil) != tt.wantErr {
				t.Errorf("gitHashObject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gitHashObject() = %v, want %v", got, tt.want)
			}
		})
	}
}

// getTraversePath gets the distance of the current working directory to the repository root.
// This is used to convert repo-relative paths to cwd-relative paths.
//
// `git rev-parse --show-cdup` always returns Unix paths, even on Windows.
func getTraversePathInTests(rootPath turbopath.AbsoluteSystemPath) (turbopath.RelativeUnixPath, error) {
	cmd := exec.Command("git", "rev-parse", "--show-cdup")
	cmd.Dir = rootPath.ToString()

	traversePath, err := cmd.Output()
	if err != nil {
		return "", err
	}

	trimmedTraversePath := strings.TrimSuffix(string(traversePath), "\n")

	return turbopath.RelativeUnixPathFromUpstream(trimmedTraversePath), nil
}

func Test_gitHashObject(t *testing.T) {
	fixturePath := getFixture(1)
	traversePath, err := getTraversePathInTests(fixturePath)
	if err != nil {
		return
	}

	tests := []struct {
		name        string
		rootPath    turbopath.AbsoluteSystemPath
		filesToHash []turbopath.AnchoredSystemPath
		want        map[turbopath.AnchoredUnixPath]string
		wantErr     bool
	}{
		{
			name:        "No paths",
			rootPath:    fixturePath,
			filesToHash: []turbopath.AnchoredSystemPath{},
			want:        map[turbopath.AnchoredUnixPath]string{},
		},
		{
			name:     "Absolute paths come back relative to rootPath",
			rootPath: fixturePath.Join("child"),
			filesToHash: []turbopath.AnchoredSystemPath{
				turbopath.AnchoredUnixPath("../root.json").ToSystemPath(),
				turbopath.AnchoredUnixPath("child.json").ToSystemPath(),
				turbopath.AnchoredUnixPath("grandchild/grandchild.json").ToSystemPath(),
			},
			want: map[turbopath.AnchoredUnixPath]string{
				"../root.json":               "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
				"child.json":                 "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
				"grandchild/grandchild.json": "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
			},
		},
		{
			name:     "Traverse outside of the repo",
			rootPath: fixturePath.Join(traversePath.ToSystemPath(), ".."),
			filesToHash: []turbopath.AnchoredSystemPath{
				turbopath.AnchoredUnixPath("null.json").ToSystemPath(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:     "Nonexistent file",
			rootPath: fixturePath,
			filesToHash: []turbopath.AnchoredSystemPath{
				turbopath.AnchoredUnixPath("nonexistent.json").ToSystemPath(),
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetHashesForFiles(tt.rootPath, tt.filesToHash)
			if (err != nil) != tt.wantErr {
				t.Errorf("gitHashObject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("gitHashObject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func requireGitCmd(t *testing.T, repoRoot turbopath.AbsoluteSystemPath, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot.ToString()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git commit failed: %v %v", err, string(out))
	}
}

func TestGetPackageDeps(t *testing.T) {
	// Directory structure:
	// <root>/
	//   new-root-file <- new file not added to git
	//   my-pkg/
	//     committed-file
	//     deleted-file
	//     uncommitted-file <- new file not added to git
	//     dir/
	//       nested-file

	repoRoot := fs.AbsoluteSystemPathFromUpstream(t.TempDir())
	myPkgDir := repoRoot.UntypedJoin("my-pkg")

	// create the dir first
	err := myPkgDir.MkdirAll(0775)
	assert.NilError(t, err, "CreateDir")

	// create file 1
	committedFilePath := myPkgDir.UntypedJoin("committed-file")
	err = committedFilePath.WriteFile([]byte("committed bytes"), 0644)
	assert.NilError(t, err, "WriteFile")

	// create file 2
	deletedFilePath := myPkgDir.UntypedJoin("deleted-file")
	err = deletedFilePath.WriteFile([]byte("delete-me"), 0644)
	assert.NilError(t, err, "WriteFile")

	// create file 3
	nestedPath := myPkgDir.UntypedJoin("dir", "nested-file")
	assert.NilError(t, nestedPath.EnsureDir(), "EnsureDir")
	assert.NilError(t, nestedPath.WriteFile([]byte("nested"), 0644), "WriteFile")

	// create a package.json
	packageJSONPath := myPkgDir.UntypedJoin("package.json")
	err = packageJSONPath.WriteFile([]byte("{}"), 0644)
	assert.NilError(t, err, "WriteFile")

	// set up git repo and commit all
	requireGitCmd(t, repoRoot, "init", ".")
	requireGitCmd(t, repoRoot, "config", "--local", "user.name", "test")
	requireGitCmd(t, repoRoot, "config", "--local", "user.email", "test@example.com")
	requireGitCmd(t, repoRoot, "add", ".")
	requireGitCmd(t, repoRoot, "commit", "-m", "foo")

	// remove a file
	err = deletedFilePath.Remove()
	assert.NilError(t, err, "Remove")

	// create another untracked file in git
	uncommittedFilePath := myPkgDir.UntypedJoin("uncommitted-file")
	err = uncommittedFilePath.WriteFile([]byte("uncommitted bytes"), 0644)
	assert.NilError(t, err, "WriteFile")

	// create an untracked file in git up a level
	rootFilePath := repoRoot.UntypedJoin("new-root-file")
	err = rootFilePath.WriteFile([]byte("new-root bytes"), 0644)
	assert.NilError(t, err, "WriteFile")

	// PackageDepsOptions are parameters for getting git hashes for a filesystem
	type PackageDepsOptions struct {
		// PackagePath is the folder path to derive the package dependencies from. This is typically the folder
		// containing package.json. If omitted, the default value is the current working directory.
		PackagePath turbopath.AnchoredSystemPath

		InputPatterns []string
	}

	tests := []struct {
		opts     *PackageDepsOptions
		expected map[turbopath.AnchoredUnixPath]string
	}{
		// base case. when inputs aren't specified, all files hashes are computed
		{
			opts: &PackageDepsOptions{
				PackagePath: "my-pkg",
			},
			expected: map[turbopath.AnchoredUnixPath]string{
				"committed-file":   "3a29e62ea9ba15c4a4009d1f605d391cdd262033",
				"uncommitted-file": "4e56ad89387e6379e4e91ddfe9872cf6a72c9976",
				"package.json":     "9e26dfeeb6e641a33dae4961196235bdb965b21b",
				"dir/nested-file":  "bfe53d766e64d78f80050b73cd1c88095bc70abb",
			},
		},
		// with inputs, only the specified inputs are hashed
		{
			opts: &PackageDepsOptions{
				PackagePath:   "my-pkg",
				InputPatterns: []string{"uncommitted-file"},
			},
			expected: map[turbopath.AnchoredUnixPath]string{
				"package.json":     "9e26dfeeb6e641a33dae4961196235bdb965b21b",
				"uncommitted-file": "4e56ad89387e6379e4e91ddfe9872cf6a72c9976",
			},
		},
		// inputs with glob pattern also works
		{
			opts: &PackageDepsOptions{
				PackagePath:   "my-pkg",
				InputPatterns: []string{"**/*-file"},
			},
			expected: map[turbopath.AnchoredUnixPath]string{
				"committed-file":   "3a29e62ea9ba15c4a4009d1f605d391cdd262033",
				"uncommitted-file": "4e56ad89387e6379e4e91ddfe9872cf6a72c9976",
				"package.json":     "9e26dfeeb6e641a33dae4961196235bdb965b21b",
				"dir/nested-file":  "bfe53d766e64d78f80050b73cd1c88095bc70abb",
			},
		},
		// inputs with traversal work
		{
			opts: &PackageDepsOptions{
				PackagePath:   "my-pkg",
				InputPatterns: []string{"../**/*-file"},
			},
			expected: map[turbopath.AnchoredUnixPath]string{
				"../new-root-file": "8906ddcdd634706188bd8ef1c98ac07b9be3425e",
				"committed-file":   "3a29e62ea9ba15c4a4009d1f605d391cdd262033",
				"uncommitted-file": "4e56ad89387e6379e4e91ddfe9872cf6a72c9976",
				"package.json":     "9e26dfeeb6e641a33dae4961196235bdb965b21b",
				"dir/nested-file":  "bfe53d766e64d78f80050b73cd1c88095bc70abb",
			},
		},
		// inputs with another glob pattern works
		{
			opts: &PackageDepsOptions{
				PackagePath:   "my-pkg",
				InputPatterns: []string{"**/{uncommitted,committed}-file"},
			},
			expected: map[turbopath.AnchoredUnixPath]string{
				"committed-file":   "3a29e62ea9ba15c4a4009d1f605d391cdd262033",
				"package.json":     "9e26dfeeb6e641a33dae4961196235bdb965b21b",
				"uncommitted-file": "4e56ad89387e6379e4e91ddfe9872cf6a72c9976",
			},
		},
		// inputs with another glob pattern + traversal work
		{
			opts: &PackageDepsOptions{
				PackagePath:   "my-pkg",
				InputPatterns: []string{"../**/{new-root,uncommitted,committed}-file"},
			},
			expected: map[turbopath.AnchoredUnixPath]string{
				"../new-root-file": "8906ddcdd634706188bd8ef1c98ac07b9be3425e",
				"committed-file":   "3a29e62ea9ba15c4a4009d1f605d391cdd262033",
				"package.json":     "9e26dfeeb6e641a33dae4961196235bdb965b21b",
				"uncommitted-file": "4e56ad89387e6379e4e91ddfe9872cf6a72c9976",
			},
		},
	}
	for _, tt := range tests {
		got, _ := GetPackageFileHashes(repoRoot, tt.opts.PackagePath, tt.opts.InputPatterns)
		assert.DeepEqual(t, got, tt.expected)
	}
}

func Test_getPackageFileHashesFromProcessingGitIgnore(t *testing.T) {
	rootIgnore := strings.Join([]string{
		"ignoreme",
		"ignorethisdir/",
	}, "\n")
	pkgIgnore := strings.Join([]string{
		"pkgignoreme",
		"pkgignorethisdir/",
	}, "\n")
	root := t.TempDir()
	repoRoot := turbopath.AbsoluteSystemPathFromUpstream(root)
	pkgName := turbopath.AnchoredUnixPath("child-dir/libA").ToSystemPath()
	type fileHash struct {
		contents string
		hash     string
	}
	files := map[turbopath.AnchoredUnixPath]fileHash{
		"top-level-file":                        {"top-level-file-contents", ""},
		"other-dir/other-dir-file":              {"other-dir-file-contents", ""},
		"ignoreme":                              {"anything", ""},
		"child-dir/libA/some-file":              {"some-file-contents", "7e59c6a6ea9098c6d3beb00e753e2c54ea502311"},
		"child-dir/libA/some-dir/other-file":    {"some-file-contents", "7e59c6a6ea9098c6d3beb00e753e2c54ea502311"},
		"child-dir/libA/some-dir/another-one":   {"some-file-contents", "7e59c6a6ea9098c6d3beb00e753e2c54ea502311"},
		"child-dir/libA/some-dir/excluded-file": {"some-file-contents", "7e59c6a6ea9098c6d3beb00e753e2c54ea502311"},
		"child-dir/libA/ignoreme":               {"anything", ""},
		"child-dir/libA/ignorethisdir/anything": {"anything", ""},
		"child-dir/libA/pkgignoreme":            {"anything", ""},
		"child-dir/libA/pkgignorethisdir/file":  {"anything", ""},
	}

	rootIgnoreFile, err := repoRoot.Join(".gitignore").Create()
	if err != nil {
		t.Fatalf("failed to create .gitignore: %v", err)
	}
	_, err = rootIgnoreFile.WriteString(rootIgnore)
	if err != nil {
		t.Fatalf("failed to write contents to .gitignore: %v", err)
	}
	err = rootIgnoreFile.Close()
	if err != nil {
		t.Fatalf("failed to close root ignore file")
	}
	pkgIgnoreFilename := pkgName.RestoreAnchor(repoRoot).Join(".gitignore")
	err = pkgIgnoreFilename.EnsureDir()
	if err != nil {
		t.Fatalf("failed to ensure directories for %v: %v", pkgIgnoreFilename, err)
	}
	pkgIgnoreFile, err := pkgIgnoreFilename.Create()
	if err != nil {
		t.Fatalf("failed to create libA/.gitignore: %v", err)
	}
	_, err = pkgIgnoreFile.WriteString(pkgIgnore)
	if err != nil {
		t.Fatalf("failed to write contents to libA/.gitignore: %v", err)
	}
	err = pkgIgnoreFile.Close()
	if err != nil {
		t.Fatalf("failed to close package ignore file")
	}
	for path, spec := range files {
		filename := path.ToSystemPath().RestoreAnchor(repoRoot)
		err = filename.EnsureDir()
		if err != nil {
			t.Fatalf("failed to ensure directories for %v: %v", filename, err)
		}
		f, err := filename.Create()
		if err != nil {
			t.Fatalf("failed to create file: %v: %v", filename, err)
		}
		_, err = f.WriteString(spec.contents)
		if err != nil {
			t.Fatalf("failed to write contents to %v: %v", filename, err)
		}
		err = f.Close()
		if err != nil {
			t.Fatalf("failed to close package ignore file")
		}
	}
	// now that we've created the repo, expect our .gitignore file too
	files[turbopath.AnchoredUnixPath("child-dir/libA/.gitignore")] = fileHash{contents: "", hash: "3237694bc3312ded18386964a855074af7b066af"}

	pkg := &fs.PackageJSON{
		Dir: pkgName,
	}
	hashes, err := GetPackageFileHashes(repoRoot, pkg.Dir, []string{})
	if err != nil {
		t.Fatalf("failed to calculate manual hashes: %v", err)
	}

	count := 0
	for path, spec := range files {
		systemPath := path.ToSystemPath()
		if systemPath.HasPrefix(pkgName) {
			relPath := systemPath[len(pkgName)+1:]
			got, ok := hashes[relPath.ToUnixPath()]
			if !ok {
				if spec.hash != "" {
					t.Errorf("did not find hash for %v, but wanted one", path)
				}
			} else if got != spec.hash {
				t.Errorf("hash of %v, got %v want %v", path, got, spec.hash)
			} else {
				count++
			}
		}
	}
	if count != len(hashes) {
		t.Errorf("found extra hashes in %v", hashes)
	}

	// expect the ignored file for manual hashing
	files[turbopath.AnchoredUnixPath("child-dir/libA/pkgignorethisdir/file")] = fileHash{contents: "anything", hash: "67aed78ea231bdee3de45b6d47d8f32a0a792f6d"}

	count = 0
	justFileHashes, err := GetPackageFileHashes(repoRoot, pkg.Dir, []string{filepath.FromSlash("**/*file"), "!" + filepath.FromSlash("some-dir/excluded-file")})
	if err != nil {
		t.Fatalf("failed to calculate manual hashes: %v", err)
	}
	for path, spec := range files {
		systemPath := path.ToSystemPath()
		if systemPath.HasPrefix(pkgName) {
			shouldInclude := strings.HasSuffix(systemPath.ToString(), "file") && !strings.HasSuffix(systemPath.ToString(), "excluded-file")
			relPath := systemPath[len(pkgName)+1:]
			got, ok := justFileHashes[relPath.ToUnixPath()]
			if !ok && shouldInclude {
				if spec.hash != "" {
					t.Errorf("did not find hash for %v, but wanted one", path)
				}
			} else if shouldInclude && got != spec.hash {
				t.Errorf("hash of %v, got %v want %v", path, got, spec.hash)
			} else if shouldInclude {
				count++
			}
		}
	}
	if count != len(justFileHashes) {
		t.Errorf("found extra hashes in %v", hashes)
	}

}

func Test_manuallyHashFiles(t *testing.T) {
	testDir := turbopath.AbsoluteSystemPath(t.TempDir())

	testFile := testDir.UntypedJoin("existing-file.txt")
	assert.NilError(t, testFile.WriteFile([]byte(""), 0644))

	type args struct {
		rootPath     turbopath.AbsoluteSystemPath
		files        []turbopath.AnchoredSystemPath
		allowMissing bool
	}
	tests := []struct {
		name    string
		args    args
		want    map[turbopath.AnchoredUnixPath]string
		wantErr bool
	}{
		// Tests for allowMissing = true
		{
			name: "allowMissing, all missing",
			args: args{
				rootPath:     testDir,
				files:        []turbopath.AnchoredSystemPath{"non-existent-file.txt"},
				allowMissing: true,
			},
			want:    map[turbopath.AnchoredUnixPath]string{},
			wantErr: false,
		},
		{
			name: "allowMissing, some missing, some not",
			args: args{
				rootPath: testDir,
				files: []turbopath.AnchoredSystemPath{
					"existing-file.txt",
					"non-existent-file.txt",
				},
				allowMissing: true,
			},
			want: map[turbopath.AnchoredUnixPath]string{
				"existing-file.txt": "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
			},
			wantErr: false,
		},
		{
			name: "allowMissing, none missing",
			args: args{
				rootPath: testDir,
				files: []turbopath.AnchoredSystemPath{
					"existing-file.txt",
				},
				allowMissing: true,
			},
			want: map[turbopath.AnchoredUnixPath]string{
				"existing-file.txt": "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
			},
			wantErr: false,
		},

		// Tests for allowMissing = false
		{
			name: "don't allowMissing, all missing",
			args: args{
				rootPath:     testDir,
				files:        []turbopath.AnchoredSystemPath{"non-existent-file.txt"},
				allowMissing: false,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "don't allowMissing, some missing, some not",
			args: args{
				rootPath: testDir,
				files: []turbopath.AnchoredSystemPath{
					"existing-file.txt",
					"non-existent-file.txt",
				},
				allowMissing: false,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "don't allowMissing, none missing",
			args: args{
				rootPath: testDir,
				files: []turbopath.AnchoredSystemPath{
					"existing-file.txt",
				},
				allowMissing: false,
			},
			want: map[turbopath.AnchoredUnixPath]string{
				"existing-file.txt": "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[turbopath.AnchoredUnixPath]string
			var err error
			if tt.args.allowMissing {
				got, err = GetHashesForExistingFiles(tt.args.rootPath, tt.args.files)
			} else {
				got, err = GetHashesForFiles(tt.args.rootPath, tt.args.files)
			}
			//got, err := manuallyHashFiles(tt.args.rootPath, tt.args.files, tt.args.allowMissing)
			if (err != nil) != tt.wantErr {
				t.Errorf("manuallyHashFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("manuallyHashFiles() = %v, want %v", got, tt.want)
			}
		})
	}
}
