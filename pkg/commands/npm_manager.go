package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jesseduffield/lazynpm/pkg/config"
	"github.com/jesseduffield/lazynpm/pkg/i18n"
	"github.com/jinzhu/copier"
	"github.com/sirupsen/logrus"
)

// NpmManager is our main git interface
type NpmManager struct {
	Log       *logrus.Entry
	OSCommand *OSCommand
	Tr        *i18n.Localizer
	Config    config.AppConfigurer
	NpmRoot   string
}

// NewNpmManager it runs git commands
func NewNpmManager(log *logrus.Entry, osCommand *OSCommand, tr *i18n.Localizer, config config.AppConfigurer) (*NpmManager, error) {
	output, err := osCommand.RunCommandWithOutput("npm root -g")
	if err != nil {
		return nil, err
	}
	npmRoot := strings.TrimSpace(output)

	return &NpmManager{
		Log:       log,
		OSCommand: osCommand,
		Tr:        tr,
		Config:    config,
		NpmRoot:   npmRoot,
	}, nil
}

func (m *NpmManager) UnmarshalPackage(r io.Reader) (*PackageConfig, error) {
	var pkgInput *PackageConfigInput
	d := json.NewDecoder(r)
	if err := d.Decode(&pkgInput); err != nil {
		return nil, err
	}

	var pkg PackageConfig
	if err := copier.Copy(&pkg, &pkgInput); err != nil {
		return nil, err
	}

	isObject := func(b []byte) bool {
		return bytes.HasPrefix(b, []byte{'{'})
	}

	if isObject(pkgInput.RawAuthor) {
		err := json.Unmarshal(pkgInput.RawAuthor, &pkg.Author)
		if err != nil {
			return nil, err
		}
	} else {
		pkg.Author.SingleLine = string(pkgInput.RawAuthor)
	}

	for _, rawContributor := range pkgInput.RawContributors {
		var contributor *Author
		if isObject(rawContributor) {
			err := json.Unmarshal(rawContributor, contributor)
			if err != nil {
				return nil, err
			}
		} else {
			contributor = &Author{SingleLine: string(rawContributor)}
		}
		pkg.Contributors = append(pkg.Contributors, *contributor)
	}

	if isObject(pkgInput.RawRepository) {
		err := json.Unmarshal(pkgInput.RawRepository, &pkg.Repository)
		if err != nil {
			return nil, err
		}
	} else {
		pkg.Repository.SingleLine = string(pkgInput.RawRepository)
	}
	return &pkg, nil
}

func (m *NpmManager) IsLinked(name string, path string) (bool, error) {
	globalPath := filepath.Join(m.NpmRoot, name)
	fileInfo, err := os.Lstat(globalPath)
	if err != nil {
		if err == os.ErrNotExist {
			return false, nil
		}
		// swallowing error. For some reason we're getting 'no such file or directory' here despite checking for os.ErrNotExist
		return false, nil
	}

	isSymlink := fileInfo.Mode()&os.ModeSymlink == os.ModeSymlink
	if isSymlink {
		linkedPath, err := os.Readlink(globalPath)
		if err != nil {
			return false, err
		}
		if linkedPath == path {
			return true, nil
		}
	}
	return false, nil
}

func (m *NpmManager) GetPackages(paths []string) ([]*Package, error) {

	pkgs := make([]*Package, 0, len(paths))

	for _, path := range paths {
		packageJsonPath := filepath.Join(path, "package.json")
		if !FileExists(packageJsonPath) {
			continue
		}

		file, err := os.OpenFile(packageJsonPath, os.O_RDONLY, 0644)
		if err != nil {
			m.Log.Error(err)
			continue
		}
		pkgConfig, err := m.UnmarshalPackage(file)

		if err != nil {
			return nil, err
		}
		linked, err := m.IsLinked(pkgConfig.Name, path)
		if err != nil {
			return nil, err
		}

		pkgs = append(pkgs, &Package{
			Config: *pkgConfig,
			Path:   path,
			Linked: linked,
		})
	}
	return pkgs, nil
}

func (m *NpmManager) ChdirToPackageRoot() (bool, error) {
	dir, err := os.Getwd()
	if err != nil {
		return false, err
	}
	for {
		if FileExists("package.json") {
			return true, nil
		}

		if err := os.Chdir(".."); err != nil {
			return false, err
		}

		newDir, err := os.Getwd()
		if err != nil {
			return false, err
		}
		if newDir == dir {
			return false, nil
		}
		dir = newDir
	}
}