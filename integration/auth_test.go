//go:build !windows
// +build !windows

package integration

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/creack/pty"
	"github.com/sclevine/spec"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

var _ = suite("auth/init", func(t *testing.T, when spec.G, it spec.S) {
	var (
		expect *require.Assertions
		server *httptest.Server
	)

	it.Before(func() {
		expect = require.New(t)

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Add("content-type", "application/json")

			switch req.URL.Path {
			case "/v1/oauth/token/info":
				auth := req.Header.Get("Authorization")

				if auth == "Bearer first-token" || auth == "Bearer second-token" ||
					auth == "Bearer some-magic-token" || auth == "Bearer some-magic-token-that-is-64-characters-long-1a1a1a1a1a11a1a1a1a1" {
					w.Write([]byte(`{"resource_owner_id":123}`))
					return
				}

				w.WriteHeader(http.StatusUnauthorized)
			case "/v2/droplets/1":
				token := req.Header.Get("Authorization")
				if token != "Bearer second-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

				w.WriteHeader(http.StatusNoContent)
			default:
				dump, err := httputil.DumpRequest(req, true)
				if err != nil {
					t.Fatal("failed to dump request")
				}

				t.Fatalf("received unknown request: %s", dump)
			}
		}))
	})

	when("a custom config is provided", func() {
		it("validates and saves the provided auth token", func() {
			tmpDir := t.TempDir()

			testConfig := filepath.Join(tmpDir, "test-config.yml")

			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"--config", testConfig,
				"auth",
				"init",
				"--token-validation-server", server.URL,
			)

			ptmx, err := pty.Start(cmd)
			expect.NoError(err)

			// Set the terminal to raw mode so that we can send the carriage return
			fd := int(ptmx.Fd())
			oldState, err := term.MakeRaw(fd)
			if err != nil {
				panic(err)
			}
			defer func() { _ = term.Restore(fd, oldState) }()

			go func() {
				ptmx.Write([]byte("some-magic-token-that-is-64-characters-long-1a1a1a1a1a11a1a1a1a1\r"))
			}()

			buf := bytes.NewBuffer([]byte{})

			count, _ := io.Copy(buf, ptmx) // yes, ignore error intentionally
			expect.NotZero(count)

			expect.Contains(buf.String(), "Validating token...")
			expect.Contains(buf.String(), "✔")

			fileBytes, err := ioutil.ReadFile(testConfig)
			expect.NoError(err)

			expect.Contains(string(fileBytes), "access-token: some-magic-token-that-is-64-characters-long-1a1a1a1a1a11a1a1a1a1")
		})
	})

	when("the --access-token flag is used", func() {
		it("validates and saves the provided token non-interactively", func() {
			tmpDir := t.TempDir()

			testConfig := filepath.Join(tmpDir, "test-config.yml")

			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"--config", testConfig,
				"auth",
				"init",
				"--access-token", "some-magic-token",
				"--token-validation-server", server.URL,
			)

			_, err := cmd.CombinedOutput()
			expect.NoError(err)

			fileBytes, err := ioutil.ReadFile(testConfig)
			expect.NoError(err)

			expect.Contains(string(fileBytes), "access-token: some-magic-token")
		})

		it("validates and overwrites an existing token non-interactively", func() {
			var testConfigBytes = []byte(`access-token: first-token
context: default
`)

			tmpDir := t.TempDir()
			testConfig := filepath.Join(tmpDir, "test-config.yml")
			expect.NoError(ioutil.WriteFile(testConfig, testConfigBytes, 0644))

			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"--config", testConfig,
				"auth",
				"init",
				"--access-token", "some-magic-token",
				"--token-validation-server", server.URL,
			)

			_, err := cmd.CombinedOutput()
			expect.NoError(err)

			fileBytes, err := ioutil.ReadFile(testConfig)
			expect.NoError(err)

			expect.Contains(string(fileBytes), "access-token: some-magic-token")
		})
	})

	when("no custom config is provided", func() {
		it("saves the auth token to the default config path", func() {
			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"auth",
				"init",
				"--token-validation-server", server.URL,
			)

			ptmx, err := pty.Start(cmd)
			expect.NoError(err)
			// Set the terminal to raw mode so that we can send the carriage return
			fd := int(ptmx.Fd())
			oldState, err := term.MakeRaw(fd)
			if err != nil {
				panic(err)
			}
			defer func() { _ = term.Restore(fd, oldState) }()

			go func() {
				ptmx.Write([]byte("some-magic-token-that-is-64-characters-long-1a1a1a1a1a11a1a1a1a1\r"))
			}()

			buf := bytes.NewBuffer([]byte{})

			count, _ := io.Copy(buf, ptmx) // yes, ignore error intentionally
			expect.NotZero(count)
			ptmx.Close()

			expect.Contains(buf.String(), "Validating token...")
			expect.Contains(buf.String(), "✔")

			location, err := getDefaultConfigLocation()
			expect.NoError(err)

			fileBytes, err := ioutil.ReadFile(location)
			expect.NoError(err)

			expect.Contains(string(fileBytes), "access-token: some-magic-token-that-is-64-characters-long-1a1a1a1a1a11a1a1a1a1")

			err = os.Remove(location)
			expect.NoError(err)
		})
	})

	when("a token cannot be validated", func() {
		it("exits non-zero with an error", func() {
			tmpDir := t.TempDir()

			testConfig := filepath.Join(tmpDir, "test-config.yml")

			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"--config", testConfig,
				"auth",
				"init",
				"--token-validation-server", server.URL,
			)

			ptmx, err := pty.Start(cmd)
			expect.NoError(err)
			// Set the terminal to raw mode so that we can send the carriage return
			fd := int(ptmx.Fd())
			oldState, err := term.MakeRaw(fd)
			if err != nil {
				panic(err)
			}
			defer func() { _ = term.Restore(fd, oldState) }()

			go func() {
				ptmx.Write([]byte("some-bad-token-that-is-64-characters-long-1a1a1a1a1a11a1a1a1a1a1\r"))
			}()

			buf := bytes.NewBuffer([]byte{})

			count, _ := io.Copy(buf, ptmx) // yes, ignore error intentionally
			expect.NotZero(count)
			ptmx.Close()

			expect.Contains(buf.String(), "Validating token...")
			expect.Contains(buf.String(), "✘")
			expect.Contains(buf.String(), fmt.Sprintf("Unable to use supplied token to access API: GET %s/v1/oauth/token/info: 401", server.URL))
		})
	})

	when("a new auth context is added", func() {
		it("allows you to switch to that context", func() {
			const nextContext = "next"

			var testConfigBytes = []byte(`access-token: first-token
auth-contexts:
  next: second-token
context: default
`)

			tmpDir := t.TempDir()
			testConfig := filepath.Join(tmpDir, "test-config.yml")
			expect.NoError(ioutil.WriteFile(testConfig, testConfigBytes, 0644))

			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"auth",
				"switch",
				"--config", testConfig,
				"--context",
				nextContext,
			)
			_, err := cmd.CombinedOutput()
			expect.NoError(err)

			fileBytes, err := ioutil.ReadFile(testConfig)
			expect.NoError(err)
			expect.Contains(string(fileBytes), "context: next")

			cmd = exec.Command(builtBinaryPath,
				"-u", server.URL,
				"--config", testConfig,
				"compute",
				"droplet",
				"delete",
				"1",
				"-f",
			)

			output, err := cmd.CombinedOutput()
			expect.NoError(err, string(output))

			err = os.Remove(testConfig)
			expect.NoError(err)
		})
	})

	when("switching contexts containing a period", func() {
		it("does not mangle that context", func() {
			var testConfigBytes = []byte(`access-token: first-token
auth-contexts:
  test@example.com: second-token
context: default
`)

			tmpDir := t.TempDir()
			testConfig := filepath.Join(tmpDir, "test-config.yml")
			expect.NoError(ioutil.WriteFile(testConfig, testConfigBytes, 0644))

			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"auth",
				"switch",
				"--config", testConfig,
			)
			_, err := cmd.CombinedOutput()
			expect.NoError(err)

			fileBytes, err := ioutil.ReadFile(testConfig)
			expect.NoError(err)
			expect.Contains(string(fileBytes), "test@example.com: second-token")

			err = os.Remove(testConfig)
			expect.NoError(err)
		})
	})

	when("the DIGITALOCEAN_CONTEXT variable is set", func() {
		it("uses that context for commands", func() {
			var testConfigBytes = []byte(`access-token: first-token
auth-contexts:
  next: second-token
context: default
`)

			tmpDir := t.TempDir()
			testConfig := filepath.Join(tmpDir, "test-config.yml")
			expect.NoError(ioutil.WriteFile(testConfig, testConfigBytes, 0644))

			cmd := exec.Command(builtBinaryPath,
				"-u", server.URL,
				"auth",
				"list",
				"--config", testConfig,
			)
			cmd.Env = os.Environ()
			cmd.Env = append(cmd.Env, "DIGITALOCEAN_CONTEXT=next")

			output, err := cmd.CombinedOutput()
			expect.NoError(err, string(output))

			expect.Contains(string(output), "next (current)")
		})
	})
})
