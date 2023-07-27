package helm3

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMixin_Build(t *testing.T) {
	ctx := context.Background()
	m := NewTestMixin(t)

	err := m.Build(ctx)
	require.NoError(t, err)

	t.Run("build with a valid config", func(t *testing.T) {
		b, err := ioutil.ReadFile("testdata/build-input-with-valid-config.yaml")
		require.NoError(t, err)

		m := NewTestMixin(t)
		m.DebugMode = false
		m.In = bytes.NewReader(b)

		err = m.Build(ctx)
		require.NoError(t, err, "build failed")

		wantOutput := fmt.Sprintf("ENV CLIENT_VERSION=%s\n", m.HelmClientVersion) +
			fmt.Sprintf("ENV API_VERSION=%s\n", m.KubernetesApiVersion) +
			fmt.Sprintf("ENV CLIENT_ARCH=%s\n", m.HelmClientArchitecture) +
			fmt.Sprintf("%s\n", PlatformConfig.Platforms[0].Init) +
			`USER ${BUNDLE_USER}
RUN helm3 repo add stable kubernetes-charts
RUN helm3 repo update
USER root
`

		gotOutput := m.TestContext.GetOutput()
		assert.Equal(t, wantOutput, gotOutput)
	})

	t.Run("build with a valid config and multiple repositories", func(t *testing.T) {
		b, err := ioutil.ReadFile("testdata/build-input-with-valid-config-multi-repos.yaml")
		require.NoError(t, err)

		m := NewTestMixin(t)
		m.DebugMode = false
		m.In = bytes.NewReader(b)

		err = m.Build(ctx)
		require.NoError(t, err, "build failed")

		wantOutput := fmt.Sprintf("ENV CLIENT_VERSION=%s\n", m.HelmClientVersion) +
			fmt.Sprintf("ENV API_VERSION=%s\n", m.KubernetesApiVersion) +
			fmt.Sprintf("ENV CLIENT_ARCH=%s\n", m.HelmClientArchitecture) +
			fmt.Sprintf("%s\n", PlatformConfig.Platforms[0].Init) +
			`USER ${BUNDLE_USER}
RUN helm3 repo add harbor https://helm.getharbor.io
RUN helm3 repo add jetstack https://charts.jetstack.io
RUN helm3 repo add stable kubernetes-charts
RUN helm3 repo update
USER root
`

		gotOutput := m.TestContext.GetOutput()
		assert.Equal(t, wantOutput, gotOutput)
	})

	t.Run("build with invalid config", func(t *testing.T) {
		b, err := ioutil.ReadFile("testdata/build-input-with-invalid-config.yaml")
		require.NoError(t, err)

		m := NewTestMixin(t)
		m.DebugMode = false
		m.In = bytes.NewReader(b)

		err = m.Build(ctx)
		require.NoError(t, err, "build failed")
		wantOutput := fmt.Sprintf("ENV CLIENT_VERSION=%s\n", m.HelmClientVersion) +
			fmt.Sprintf("ENV API_VERSION=%s\n", m.KubernetesApiVersion) +
			fmt.Sprintf("ENV CLIENT_ARCH=%s\n", m.HelmClientArchitecture) +
			fmt.Sprintf("%s\n", PlatformConfig.Platforms[0].Init) +
			`USER ${BUNDLE_USER}
RUN helm3 repo update
USER root
`
		gotOutput := m.TestContext.GetOutput()
		assert.Equal(t, wantOutput, gotOutput)
	})

	t.Run("build with supressed initial lines", func(t *testing.T) {
		b, err := ioutil.ReadFile("testdata/build-input-with-none-imageplatform.yaml")
		require.NoError(t, err)

		m := NewTestMixin(t)
		m.DebugMode = false
		m.In = bytes.NewReader(b)

		err = m.Build(ctx)
		require.NoError(t, err, "build failed")
		wantOutput := "# helm mixin buildtime ouput was supressed\n"

		gotOutput := m.TestContext.GetOutput()
		assert.Equal(t, wantOutput, gotOutput)
	})

	t.Run("build with a defined helm client version", func(t *testing.T) {

		b, err := ioutil.ReadFile("testdata/build-input-with-version.yaml")
		require.NoError(t, err)

		m := NewTestMixin(t)
		m.DebugMode = false
		m.In = bytes.NewReader(b)
		err = m.Build(ctx)
		require.NoError(t, err, "build failed")
		wantOutput := fmt.Sprintf("ENV CLIENT_VERSION=%s\n", m.HelmClientVersion) +
			fmt.Sprintf("ENV API_VERSION=%s\n", m.KubernetesApiVersion) +
			fmt.Sprintf("ENV CLIENT_ARCH=%s\n", m.HelmClientArchitecture) +
			fmt.Sprintf("%s\n", PlatformConfig.Platforms[0].Init)
		gotOutput := m.TestContext.GetOutput()
		assert.Equal(t, wantOutput, gotOutput)
	})

	t.Run("build with a defined helm client version that does not meet the semver constraint", func(t *testing.T) {

		b, err := ioutil.ReadFile("testdata/build-input-with-unsupported-client-version.yaml")
		require.NoError(t, err)

		m := NewTestMixin(t)
		m.DebugMode = false
		m.In = bytes.NewReader(b)
		err = m.Build(ctx)
		require.EqualError(t, err, `supplied clientVersion "v2.16.1" does not meet semver constraint "^v3.x"`)
	})

	t.Run("build with a defined helm client version that does not parse as valid semver", func(t *testing.T) {

		b, err := ioutil.ReadFile("testdata/build-input-with-invalid-client-version.yaml")
		require.NoError(t, err)

		m := NewTestMixin(t)
		m.DebugMode = false
		m.In = bytes.NewReader(b)
		err = m.Build(ctx)
		require.EqualError(t, err, `supplied client version "v3.8.2.0" cannot be parsed as semver: Invalid Semantic Version`)
	})

}
