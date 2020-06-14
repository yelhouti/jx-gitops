package recreate

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/jenkins-x/jx-gitops/pkg/common"
	"github.com/jenkins-x/jx/pkg/cmd/helper"
	"github.com/jenkins-x/jx/pkg/cmd/templates"
	"github.com/jenkins-x/jx/pkg/log"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

var (
	kptLong = templates.LongDesc(`
		Updates the kpt packages in the given directory
`)

	kptExample = templates.Examples(`
		# updates the kpt of all the yaml resources in the given directory
		%s kpt --dir .
	`)

	pathSeparator = string(os.PathSeparator)
)

// KptOptions the options for the command
type Options struct {
	Dir           string
	OutDir        string
	CommandRunner common.CommandRunner
}

// NewCmdKptRecreate creates a command object for the command
func NewCmdKptRecreate() (*cobra.Command, *Options) {
	o := &Options{}

	cmd := &cobra.Command{
		Use:     "recreate",
		Short:   "Recreates the kpt packages in the given directory",
		Long:    kptLong,
		Example: fmt.Sprintf(kptExample, common.BinaryName),
		Run: func(cmd *cobra.Command, args []string) {
			err := o.Run()
			helper.CheckErr(err)
		},
	}
	cmd.Flags().StringVarP(&o.Dir, "dir", "d", ".", "the directory to recursively look for the *.yaml or *.yml files")
	cmd.Flags().StringVarP(&o.OutDir, "out-dir", "o", "", "the output directory to generate the output")
	return cmd, o
}

// Run implements the command
func (o *Options) Run() error {
	if o.Dir == "" {
		o.Dir = "."
	}
	dir, err := filepath.Abs(o.Dir)
	if err != nil {
		return errors.Wrapf(err, "failed to find abs dir of %s", o.Dir)
	}

	if o.OutDir == "" {
		o.OutDir, err = ioutil.TempDir("", "")
		if err != nil {
			return errors.Wrap(err, "failed to create temp dir")
		}
	}
	if o.CommandRunner == nil {
		o.CommandRunner = common.DefaultCommandRunner
	}

	err = util.CopyDirOverwrite(dir, o.OutDir)
	if err != nil {
		return errors.Wrapf(err, "failed to copy %s to %s", dir, o.OutDir)
	}
	dir = o.OutDir

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info == nil || info.IsDir() {
			return nil
		}
		kptDir, name := filepath.Split(path)
		if name != "Kptfile" {
			return nil
		}
		rel, err := filepath.Rel(dir, kptDir)
		if err != nil {
			return errors.Wrapf(err, "failed to calculate the relative directory of %s", kptDir)
		}
		kptDir = strings.TrimSuffix(kptDir, pathSeparator)
		parentDir, _ := filepath.Split(kptDir)
		parentDir = strings.TrimSuffix(parentDir, pathSeparator)

		u := &unstructured.Unstructured{}
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return errors.Wrapf(err, "failed to read file %s", path)
		}
		err = yaml.Unmarshal(data, u)
		if err != nil {
			return errors.Wrapf(err, "failed to parse URL for %s", path)
		}

		gitURL, _, err := unstructured.NestedString(u.Object, "upstream", "git", "repo")
		if err != nil {
			return errors.Wrapf(err, "failed to find git URL for path %s", path)
		}
		if gitURL == "" {
			return errors.Errorf("no git URL for path %s", path)
		}
		directory, _, err := unstructured.NestedString(u.Object, "upstream", "git", "directory")
		if err != nil {
			return errors.Wrapf(err, "failed to find git directory for path %s", path)
		}
		if directory == "" {
			return errors.Errorf("no git directory for path %s", path)
		}
		version, _, err := unstructured.NestedString(u.Object, "upstream", "git", "commit")
		if err != nil {
			return errors.Wrapf(err, "failed to find git commit for path %s", path)
		}
		if version == "" {
			return errors.Errorf("no git version for path %s", path)
		}

		if !strings.HasSuffix(gitURL, ".git") {
			gitURL = strings.TrimSuffix(gitURL, "/") + ".git"
		}
		if !strings.HasPrefix(directory, pathSeparator) {
			directory = pathSeparator + directory
		}

		expression := fmt.Sprintf("%s%s@%s", gitURL, directory, version)
		args := []string{"pkg", "get", expression, rel}
		c := &util.Command{
			Name: "kpt",
			Args: args,
			Dir:  dir,
		}

		err = os.RemoveAll(kptDir)
		if err != nil {
			return errors.Wrapf(err, "failed to remove kpt directory %s", kptDir)
		}
		log.Logger().Infof("about to run %s in dir %s", util.ColorInfo(c.String()), util.ColorInfo(c.Dir))
		text, err := o.CommandRunner(c)
		log.Logger().Infof(text)
		if err != nil {
			return errors.Wrapf(err, "failed to run kpt command")
		}
		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "failed to upgrade kpt packages in dir %s", dir)
	}
	return nil
}
