// Package helm checks the cactusInv Helm chart (M60) without Helm: it renders every
// template with the few Helm functions the chart uses (the chart keeps to them) and parses
// each manifest, so `make check` catches a broken template or a missing value.
package helm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"go.yaml.in/yaml/v3"
)

const chart = "cactusinv"

func render(t *testing.T, overrides map[string]any) ([]map[string]any, error) {
	t.Helper()
	var values map[string]any
	raw, err := os.ReadFile(filepath.Join(chart, "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	merge(values, overrides)
	var meta map[string]any
	raw, err = os.ReadFile(filepath.Join(chart, "Chart.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	ctx := map[string]any{
		"Values":  values,
		"Release": map[string]any{"Name": "inv", "Namespace": "billing"},
		"Chart":   map[string]any{"Name": meta["name"], "AppVersion": meta["appVersion"], "Version": meta["version"]},
	}

	root := template.New("chart").Option("missingkey=zero")
	root.Funcs(funcs(root))
	files, _ := filepath.Glob(filepath.Join(chart, "templates", "*"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := root.New(filepath.Base(f)).Parse(string(b)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	var docs []map[string]any
	for _, f := range files {
		name := filepath.Base(f)
		if strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		var out bytes.Buffer
		if err := root.ExecuteTemplate(&out, name, ctx); err != nil {
			return nil, err
		}
		for _, part := range strings.Split("\n"+out.String(), "\n---") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			var doc map[string]any
			if err := yaml.Unmarshal([]byte(part), &doc); err != nil {
				t.Fatalf("%s: invalid YAML: %v\n%s", name, err, part)
			}
			if doc["apiVersion"] == nil || doc["kind"] == nil || dig(doc, "metadata", "name") == nil {
				t.Fatalf("%s: a manifest without apiVersion, kind or name:\n%s", name, part)
			}
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func funcs(root *template.Template) template.FuncMap {
	return template.FuncMap{
		"include": func(name string, data any) (string, error) {
			var b bytes.Buffer
			err := root.ExecuteTemplate(&b, name, data)
			return b.String(), err
		},
		"required": func(msg string, v any) (any, error) {
			if v == nil || v == "" {
				return nil, errors.New(msg)
			}
			return v, nil
		},
		"default": func(d, v any) any {
			if v == nil || v == "" {
				return d
			}
			return v
		},
		"quote": func(v any) string {
			if v == nil {
				return `""`
			}
			return fmt.Sprintf("%q", fmt.Sprint(v))
		},
		"toYaml": func(v any) string {
			b, err := yaml.Marshal(v)
			if err != nil {
				return ""
			}
			return strings.TrimSuffix(string(b), "\n")
		},
		"nindent": func(n int, s string) string {
			pad := strings.Repeat(" ", n)
			return "\n" + pad + strings.ReplaceAll(s, "\n", "\n"+pad)
		},
		"dict": func(kv ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
		"sha256sum": func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) },
		"trunc": func(n int, s string) string {
			if len(s) > n {
				return s[:n]
			}
			return s
		},
	}
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func dig(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}

func kinds(docs []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, d := range docs {
		out[fmt.Sprint(d["kind"])+"/"+fmt.Sprint(dig(d, "metadata", "name"))] = d
	}
	return out
}

var required = map[string]any{
	"image":   map[string]any{"repository": "registry.example.com/cactusinv"},
	"secrets": map[string]any{"dbPassword": "db", "dbRootPassword": "root", "appKey": strings.Repeat("ab", 32), "adminPassword": "first-password-1"},
}

func TestChartRendersWithItsDefaults(t *testing.T) {
	docs, err := render(t, maps.Clone(required))
	if err != nil {
		t.Fatal(err)
	}
	got := kinds(docs)
	for _, want := range []string{
		"Secret/inv-secrets", "ConfigMap/inv-config", "Deployment/inv", "Service/inv", "Deployment/inv-mariadb", "Service/inv-mariadb",
		"PersistentVolumeClaim/inv-mariadb-data", "PersistentVolumeClaim/inv-invoices", "PersistentVolumeClaim/inv-backups",
	} {
		if got[want] == nil {
			t.Errorf("missing %s", want)
		}
	}
	if got["Ingress/inv"] != nil || got["Deployment/inv-mailpit"] != nil {
		t.Error("ingress and mailpit are off by default")
	}
	app := got["Deployment/inv"]
	c := dig(app, "spec", "template", "spec", "containers").([]any)[0].(map[string]any)
	if c["image"] != "registry.example.com/cactusinv:1.35.0" {
		t.Errorf("image = %v", c["image"])
	}
	cm := got["ConfigMap/inv-config"]["data"].(map[string]any)
	if cm["CACTUSINV_DB_HOST"] != "inv-mariadb" || cm["CACTUSINV_COOKIE_SECURE"] != "true" || cm["OTEL_EXPORTER_OTLP_ENDPOINT"] != "" {
		t.Errorf("config = %v", cm)
	}
}

func TestChartOptions(t *testing.T) {
	o := maps.Clone(required)
	o["secrets"] = map[string]any{"existingSecret": "my-secrets"}
	o["mailpit"] = map[string]any{"enabled": true}
	o["ingress"] = map[string]any{"enabled": true, "host": "inv.example.com", "tlsSecret": "inv-tls", "className": "traefik"}
	o["service"] = map[string]any{"type": "NodePort", "nodePort": 30080}
	o["config"] = map[string]any{"extraEnv": map[string]any{"CACTUSINV_EXTRA": "1"}}
	docs, err := render(t, o)
	if err != nil {
		t.Fatal(err)
	}
	got := kinds(docs)
	if got["Secret/inv-secrets"] != nil || got["Ingress/inv"] == nil || got["Deployment/inv-mailpit"] == nil {
		t.Fatalf("rendered %v", keys(got))
	}
	vols := dig(got["Deployment/inv"], "spec", "template", "spec", "volumes").([]any)
	if dig(vols[2].(map[string]any), "secret", "secretName") != "my-secrets" {
		t.Errorf("secret volume = %v", vols[2])
	}
	cm := got["ConfigMap/inv-config"]["data"].(map[string]any)
	if cm["CACTUSINV_SMTP_HOST"] != "inv-mailpit" || cm["CACTUSINV_EXTRA"] != "1" {
		t.Errorf("config = %v", cm)
	}
	port := dig(got["Service/inv"], "spec", "ports").([]any)[0].(map[string]any)
	if port["nodePort"] != 30080 {
		t.Errorf("service port = %v", port)
	}
}

func TestChartRequiresImageAndSecrets(t *testing.T) {
	if _, err := render(t, map[string]any{"secrets": required["secrets"]}); err == nil || !strings.Contains(err.Error(), "image.repository is required") {
		t.Errorf("no image: %v", err)
	}
	if _, err := render(t, map[string]any{"image": required["image"]}); err == nil || !strings.Contains(err.Error(), "secrets.dbPassword is required") {
		t.Errorf("no secrets: %v", err)
	}
}

func keys(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The chart's appVersion is the release's: a version bump must not leave it behind.
func TestChartAppVersionIsTheRelease(t *testing.T) {
	v, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	raw, _ := os.ReadFile(filepath.Join(chart, "Chart.yaml"))
	if err := yaml.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["appVersion"] != strings.TrimSpace(string(v)) {
		t.Fatalf("Chart.yaml appVersion %v, VERSION %s", meta["appVersion"], v)
	}
}
