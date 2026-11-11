// Package hook is the Claude Code side of Kōwhai. It runs on the
// developer's machine as `kowhai hook` and `kowhai link`.
package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ProjectFile is the name of the per-project link file. It holds no
// secrets and can be committed.
const ProjectFile = ".kowhai"

// Project is the content of a .kowhai file.
type Project struct {
	Server string `toml:"server"`
	Card   string `toml:"card"`
}

// UserConfig is ~/.config/kowhai/config.toml.
type UserConfig struct {
	Server string `toml:"server"`
	Token  string `toml:"token"`
}

// FindProject looks for .kowhai in dir and its parents.
func FindProject(dir string) (Project, string, error) {
	for {
		path := filepath.Join(dir, ProjectFile)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			var p Project
			if _, err := toml.DecodeFile(path, &p); err != nil {
				return p, path, fmt.Errorf("%s: %w", path, err)
			}
			return p, path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Project{}, "", os.ErrNotExist
		}
		dir = parent
	}
}

func configDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "kowhai")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "kowhai")
}

func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "kowhai")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "kowhai")
}

// LoadUserConfig reads the user config. A missing file is not an error.
func LoadUserConfig() UserConfig {
	var c UserConfig
	_, _ = toml.DecodeFile(filepath.Join(configDir(), "config.toml"), &c)
	if v := os.Getenv("KOWHAI_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("KOWHAI_URL"); v != "" {
		c.Server = v
	}
	return c
}

// Client is a small REST client.
type Client struct {
	Server string
	Token  string
	HTTP   *http.Client
}

// APIError is a problem response from the server.
type APIError struct {
	Status int
	Title  string
	Detail string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%d %s: %s", e.Status, e.Title, e.Detail)
	}
	return fmt.Sprintf("%d %s", e.Status, e.Title)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Server, "/")+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		e := &APIError{Status: resp.StatusCode}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(e)
		if e.Title == "" {
			e.Title = http.StatusText(resp.StatusCode)
		}
		return e
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func pathEsc(s string) string { return url.PathEscape(s) }

var errNoToken = errors.New("no token: set KOWHAI_TOKEN or add token to ~/.config/kowhai/config.toml")
