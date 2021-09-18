package helm3

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
)

type InstallAction struct {
	Steps []InstallStep `yaml:"install"`
}

type InstallStep struct {
	InstallArguments `yaml:"helm3"`
}

type InstallArguments struct {
	Step `yaml:",inline"`

	Namespace        string            `yaml:"namespace"`
	Name             string            `yaml:"name"`
	Chart            string            `yaml:"chart"`
	DependencyUpdate bool              `yaml:"dependencyupdate`
	Devel            bool              `yaml:"devel`
	NoHooks          bool              `yaml:"nohooks"`
	Repo             string            `yaml:"repo"`
	Replace          bool              `yaml:"replace"`
	Set              map[string]string `yaml:"set"`
	Password         string            `yaml:"password"`
	Username         string            `yaml:"username"`
	Upsert           bool              `yaml:"upsert"`
	Values           []string          `yaml:"values"`
	Version          string            `yaml:"version"`
	Wait             bool              `yaml:"wait"`
}

func (m *Mixin) Install() error {

	payload, err := m.getPayloadData()
	if err != nil {
		return err
	}

	kubeClient, err := m.getKubernetesClient("/root/.kube/config")
	if err != nil {
		return errors.Wrap(err, "couldn't get kubernetes client")
	}

	var action InstallAction
	err = yaml.Unmarshal(payload, &action)
	if err != nil {
		return err
	}
	if len(action.Steps) != 1 {
		return errors.Errorf("expected a single step, but got %d", len(action.Steps))
	}
	step := action.Steps[0]

	cmd := m.NewCommand("helm3")

	if step.Upsert {
		cmd.Args = append(cmd.Args, "upgrade", "--install", step.Name, step.Chart)
	} else {
		cmd.Args = append(cmd.Args, "install", step.Name, step.Chart)
	}

	cmd.Args = append(cmd.Args, "upgrade", "--install", step.Name, step.Chart)

	if step.Namespace != "" {
		cmd.Args = append(cmd.Args, "--namespace", step.Namespace)
	}

	if step.Version != "" {
		cmd.Args = append(cmd.Args, "--version", step.Version)
	}

	if !step.Upsert && step.Replace {
		cmd.Args = append(cmd.Args, "--replace")
	}

	if step.Wait {
		cmd.Args = append(cmd.Args, "--wait")
	}

	if step.Devel {
		cmd.Args = append(cmd.Args, "--devel")
	}

	for _, v := range step.Values {
		cmd.Args = append(cmd.Args, "--values", v)
	}

	if step.DependencyUpdate {
		cmd.Args = append(cmd.Args, "--dependency-update")
	}

	if step.NoHooks {
		cmd.Args = append(cmd.Args, "--no-hooks")
	}

	// This will ensure the installation process deletes the installation on failure.
	cmd.Args = append(cmd.Args, "--atomic")
	// This will ensure the creation of the release namespace if not present.
	cmd.Args = append(cmd.Args, "--create-namespace")
	// Set values
	cmd.Args = HandleSettingChartValuesForInstall(step, cmd)

	cmd.Stdout = m.Out
	cmd.Stderr = m.Err

	// format the command with all arguments
	prettyCmd := fmt.Sprintf("%s %s", cmd.Path, strings.Join(cmd.Args, " "))
	fmt.Fprintln(m.Out, prettyCmd)

	// Here where really the command get executed
	err = cmd.Start()
	// Exit on error
	if err != nil {
		return fmt.Errorf("could not execute command, %s: %s", prettyCmd, err)
	}
	err = cmd.Wait()
	// Exit on error
	if err != nil {
		return err
	}
	err = m.handleOutputs(kubeClient, step.Namespace, step.Outputs)
	return err
}

// Prepare set arguments
func HandleSettingChartValuesForInstall(step InstallStep, cmd *exec.Cmd) []string {
	// sort the set consistently
	setKeys := make([]string, 0, len(step.Set))
	for k := range step.Set {

		setKeys = append(setKeys, k)
	}
	sort.Strings(setKeys)

	for _, k := range setKeys {
		cmd.Args = append(cmd.Args, "--set", fmt.Sprintf("%s=%s", k, step.Set[k]))
	}
	return cmd.Args
}
