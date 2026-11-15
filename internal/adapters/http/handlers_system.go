package httpapi

import (
	"context"
	"time"

	"github.com/albievan/cactusinv/internal/ports"
)

// The Database & containers page, its secrets and the dashboard's health (M57).

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func toAPISettings(in []ports.Setting) []Setting {
	out := make([]Setting, len(in))
	for i, s := range in {
		out[i] = Setting{Name: s.Name, Value: s.Value}
	}
	return out
}

func toAPIRuntime(r ports.RuntimeStats) RuntimeInfo {
	out := RuntimeInfo{
		Goroutines: r.Goroutines, HeapBytes: int64(r.HeapBytes), SysBytes: int64(r.SysBytes), GcRuns: int64(r.GCRuns), Cpus: r.CPUs, //nolint:gosec // memory sizes fit in int64
		GoVersion: r.GoVersion, Os: r.OS, Arch: r.Arch, Hostname: r.Hostname, StartedAt: r.StartedAt,
	}
	return out
}

func toAPIPool(p ports.DBPool) DBPool {
	return DBPool{MaxOpen: p.MaxOpen, Open: p.Open, InUse: p.InUse, Idle: p.Idle, WaitCount: p.WaitCount, WaitMs: p.WaitDuration.Milliseconds()}
}

func (h *handlers) GetSystemStatus(ctx context.Context, _ GetSystemStatusRequestObject) (GetSystemStatusResponseObject, error) {
	st, err := h.svc.SystemStatus(ctx)
	if err != nil {
		return nil, err
	}
	d := st.Database
	out := GetSystemStatus200JSONResponse{
		Target: st.Target, Version: st.Version, Build: st.Build, Container: st.Container, Runtime: toAPIRuntime(st.Runtime),
		UptimeSeconds: int64(st.Uptime.Seconds()),
		Database: DatabaseInfo{
			Host: st.DBHost, Port: st.DBPort, Name: st.DBName, User: st.DBUser, Error: st.DatabaseError,
			Version: d.Version, VersionComment: d.VersionComment, UptimeSeconds: int64(d.Uptime.Seconds()), PingMs: ms(d.Ping),
			SizeBytes: d.SizeBytes, Tables: d.Tables, Connections: d.Connections, MaxConnections: d.MaxConnections,
			MaxUsedConnections: d.MaxUsedConnections, Queries: d.Queries, SlowQueries: d.SlowQueries,
			Charset: d.Charset, Collation: d.Collation, TimeZone: d.TimeZone, Pool: toAPIPool(d.Pool),
			Variables: toAPISettings(d.Variables), Status: toAPISettings(d.Status),
		},
		Services: make([]ServiceInfo, len(st.Services)), Configuration: toAPISettings(st.Configuration), Secrets: make([]SecretInfo, len(st.Secrets)),
	}
	for i, s := range st.Services {
		out.Services[i] = ServiceInfo{Name: s.Name, Role: s.Role, State: ServiceInfoState(s.State), Detail: s.Detail, LatencyMs: ms(s.Latency)}
	}
	for i, s := range st.Secrets {
		out.Secrets[i] = SecretInfo{Key: s.Key, Description: s.Description, Present: s.Present}
	}
	return out, nil
}

func (h *handlers) RevealSecret(ctx context.Context, req RevealSecretRequestObject) (RevealSecretResponseObject, error) {
	v, err := h.svc.RevealSecret(ctx, req.Body.Key, req.Body.CurrentPassword)
	if err != nil {
		return nil, err
	}
	return RevealSecret200JSONResponse{Key: req.Body.Key, Value: v}, nil
}

func (h *handlers) GetSystemHealth(ctx context.Context, _ GetSystemHealthRequestObject) (GetSystemHealthResponseObject, error) {
	hl, err := h.svc.SystemHealth(ctx)
	if err != nil {
		return nil, err
	}
	r := hl.Requests
	out := GetSystemHealth200JSONResponse{
		State: SystemHealthState(hl.State), Checks: make([]HealthCheck, len(hl.Checks)), UptimeSeconds: int64(hl.Uptime.Seconds()),
		Runtime: toAPIRuntime(hl.Runtime), DbPingMs: ms(hl.DBPing), Pool: toAPIPool(hl.Pool),
		Requests: RequestInfo{
			Total: r.Total, ClientErrors: r.ClientErrors, ServerErrors: r.ServerErrors, AverageMs: ms(r.Average), P95Ms: ms(r.P95), LastMinute: r.LastMinute,
		},
		OutboxPending: hl.OutboxPending, OutboxFailed: hl.OutboxFailed,
	}
	for i, c := range hl.Checks {
		out.Checks[i] = HealthCheck{Name: c.Name, State: HealthCheckState(c.State), Detail: c.Detail}
	}
	return out, nil
}
