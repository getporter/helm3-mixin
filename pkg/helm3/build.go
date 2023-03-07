package helm3

import (
	"context"
	"fmt"
	"github.com/imdario/mergo"
	"log"
	"os"
	"sort"
	"strings"

	"get.porter.sh/porter/pkg/exec/builder"
	"github.com/Masterminds/semver"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

// clientVersionConstraint represents the semver constraint for the Helm client version
// Currently, this mixin only supports Helm clients versioned v3.x.x
const clientVersionConstraint string = "^v3.x"

// BuildInput represents stdin passed to the mixin for the build command.
type BuildInput struct {
	Config MixinConfig `yaml:"config"`
}

// MixinConfig represents configuration that can be set on the helm3 mixin in porter.yaml
// mixins:
// - helm3:
// 	  clientVersion: v3.8.2
// 	  apiVersion: v1.22.1
// 	  clientArchitecture: amd64 | arm64 | arm
// 	  imagePlatform: default | debian | centos | none
//	  repositories:
//	    stable:
//		  url: "https://charts.helm.sh/stable"

type MixinConfig struct {
	ClientVersion      string                `yaml:"clientVersion,omitempty"`
	ApiVersion         string                `yaml:"apiVersion,omitempty"`
	ClientArchitecture string                `yaml:"clientArchitecture,omitempty"`
	ImagePlatform      string                `yaml:"imagePlatform,omitempty"`
	Repositories       map[string]Repository `yaml:"repositories,omitempty"`
}

type Repository struct {
	URL string `yaml:"url,omitempty"`
}

// Config with Dockerfile lines for other platforms
const mixinConfigSuffix string = "mixins/helm3/config.yaml"

type Platform struct {
	Name string `yaml:"name"`
	Init string `yaml:"init"`
}

type Config struct {
	Platforms []Platform `yaml:"platforms"`
}

// Build will generate the necessary Dockerfile lines
// for an invocation image using this mixin
func (m *Mixin) Build(ctx context.Context) error {

	// make master configures
	platformConfig := Config{
		Platforms: []Platform{
			{
				Name: "default",
				Init: `ENV HELM_EXPERIMENTAL_OCI=1
RUN apt-get update && apt-get install -y curl
RUN curl https://get.helm.sh/helm-${CLIENT_VERSION}-linux-${CLIENT_ARCH}.tar.gz --output helm3.tar.gz
RUN tar -xvf helm3.tar.gz && rm helm3.tar.gz
RUN mv linux-${CLIENT_ARCH}/helm /usr/local/bin/helm3
RUN curl -o kubectl https://storage.googleapis.com/kubernetes-release/release/${API_VERSION}/bin/linux/${CLIENT_ARCH}/kubectl &&\
    mv kubectl /usr/local/bin && chmod a+x /usr/local/bin/kubectl`,
			},
		},
	}

	mainConfig := BuildInput{
		Config: MixinConfig{
			ClientVersion:      defaultClientVersion,
			ApiVersion:         defaultApiVersion,
			ClientArchitecture: defaultClientArchitecture,
			ImagePlatform:      defaultImagePlatform,
		},
	}

	mixinConfigPath := ""
	for _, e := range os.Environ() {
		pair := strings.SplitN(e, "=", 2)
		if pair[0] == "PORTER_HOME" {
			mixinConfigPath = pair[1] + "/" + mixinConfigSuffix
		}
	}
	if mixinConfigPath == "/"+mixinConfigSuffix {
		log.Fatalln("Porter home not found")
	}
	if _, err := os.Stat(mixinConfigPath); errors.Is(err, os.ErrNotExist) {
		// save default config
		fs, err := os.Create(mixinConfigPath)
		if err != nil {
			log.Fatal(err)
		}
		defer fs.Close()
		content, _ := yaml.Marshal(platformConfig)
		fmt.Fprintln(fs, string(content))
	} else {
		// read and merge custom config
		fc, err := os.ReadFile(mixinConfigPath)
		if err != nil {
			log.Fatal(err)
		}
		var customConfig Config
		err = yaml.Unmarshal(fc, &customConfig)
		if err != nil {
			log.Fatal(err)
		}
		// note, key "platforms" will be overwritten!
		if err = mergo.Merge(&platformConfig, customConfig, mergo.WithOverride); err != nil {
			log.Fatal(err)
		}
	}

	// Create new Builder.
	var inputConfig BuildInput
	// get contents from porter.yaml
	err := builder.LoadAction(ctx, m.RuntimeConfig, "", func(contents []byte) (interface{}, error) {
		// unmarshal mixin config
		err := yaml.Unmarshal(contents, &inputConfig)
		return &inputConfig, err
	})
	if err != nil {
		return err
	}

	// merge custom config and main config
	if err = mergo.Merge(&mainConfig, inputConfig, mergo.WithOverride); err != nil {
		log.Fatal(err)
	}

	suppliedClientVersion := mainConfig.Config.ClientVersion
	if suppliedClientVersion != "" {
		ok, err := validate(suppliedClientVersion, clientVersionConstraint)
		if err != nil {
			return err
		}
		if !ok {
			return errors.Errorf("supplied clientVersion %q does not meet semver constraint %q",
				suppliedClientVersion, clientVersionConstraint)
		}
		m.HelmClientVersion = suppliedClientVersion
	}

	// fix helm3.go structure Mixin
	m.HelmClientVersion = mainConfig.Config.ClientVersion
	m.HelmClientArchitecture = mainConfig.Config.ClientArchitecture
	m.KubernetesApiVersion = mainConfig.Config.ApiVersion
	m.InvocationImagePlatform = mainConfig.Config.ImagePlatform

	// ImagePlatform == "none" is reserved word for supress generation
	if mainConfig.Config.ImagePlatform == "none" {
		fmt.Fprintln(m.Out, "# helm mixin buildtime ouput was supressed")
		return nil
	}

	// Add environment variables
	fmt.Fprintf(m.Out, "ENV CLIENT_VERSION=%s\n", mainConfig.Config.ClientVersion)
	fmt.Fprintf(m.Out, "ENV API_VERSION=%s\n", mainConfig.Config.ApiVersion)
	fmt.Fprintf(m.Out, "ENV CLIENT_ARCH=%s\n", mainConfig.Config.ClientArchitecture)

	//Insert initial lines for actual image platform
	for _, item := range platformConfig.Platforms {
		if item.Name == mainConfig.Config.ImagePlatform {
			fmt.Fprintln(m.Out, item.Init)
		}
	}

	if len(mainConfig.Config.Repositories) > 0 {
		// Switch to a non-root user so helm is configured for the user the container will execute as
		fmt.Fprintln(m.Out, "USER ${BUNDLE_USER}")

		// Go through repositories
		names := make([]string, 0, len(mainConfig.Config.Repositories))
		for name := range mainConfig.Config.Repositories {
			names = append(names, name)
		}
		sort.Strings(names) //sort by key
		for _, name := range names {
			url := mainConfig.Config.Repositories[name].URL
			repositoryCommand, err := getRepositoryCommand(name, url)
			if err != nil {
				if m.DebugMode {
					fmt.Fprintf(m.Err, "DEBUG: addition of repository failed: %s\n", err.Error())
				}
			} else {
				fmt.Fprintln(m.Out, strings.Join(repositoryCommand, " "))
			}
		}
		// Make sure we update  the helm repositories
		// So we don\'t have to do it later
		fmt.Fprintln(m.Out, "RUN helm3 repo update")

		// Switch back to root so that subsequent mixins can install things
		fmt.Fprintln(m.Out, "USER root")
	}

	return nil
}

func getRepositoryCommand(name, url string) (repositoryCommand []string, err error) {

	var commandBuilder []string

	if url == "" {
		return commandBuilder, fmt.Errorf("repository url must be supplied")
	}

	commandBuilder = append(commandBuilder, "RUN", "helm3", "repo", "add", name, url)

	return commandBuilder, nil
}

// validate validates that the supplied clientVersion meets the supplied semver constraint
func validate(clientVersion, constraint string) (bool, error) {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, errors.Wrapf(err, "unable to parse version constraint %q", constraint)
	}

	v, err := semver.NewVersion(clientVersion)
	if err != nil {
		return false, errors.Wrapf(err, "supplied client version %q cannot be parsed as semver", clientVersion)
	}

	return c.Check(v), nil
}
