package service

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	RecentCallsContextKeyID = "recent_calls_id"

	DefaultRecentCallsCapacity = 100

	DefaultMaxRequestBodyBytes  = 8 << 20   // 8MiB
	DefaultMaxResponseBodyBytes = 256 << 10 // 256KiB

	DefaultMaxStreamChunkBytes = 8 << 10   // 8KiB
	DefaultMaxStreamTotalBytes = 256 << 10 // 256KiB
)

type RecentCallsCacheConfig struct {
	Capacity int

	MaxRequestBodyBytes  int
	MaxResponseBodyBytes int

	MaxStreamChunkBytes int
	MaxStreamTotalBytes int
}

type RecentCallRequest struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Header map[string]string `json:"headers,omitempty"`

	BodyType   string `json:"body_type,omitempty"` // json/text/binary/unknown/omitted
	Body       string `json:"body,omitempty"`      // truncated string or base64 (when BodyType=binary)
	Truncated  bool   `json:"truncated,omitempty"` // body truncated
	Omitted    bool   `json:"omitted,omitempty"`   // body not recorded
	OmitReason string `json:"omit_reason,omitempty"`
}

type RecentCallUpstreamResponse struct {
	StatusCode int               `json:"status_code"`
	Header     map[string]string `json:"headers,omitempty"`

	BodyType   string `json:"body_type,omitempty"` // json/text/binary/unknown/omitted
	Body       string `json:"body,omitempty"`      // raw upstream body (string or base64)
	Truncated  bool   `json:"truncated,omitempty"`
	Omitted    bool   `json:"omitted,omitempty"`
	OmitReason string `json:"omit_reason,omitempty"`
}

type RecentCallUpstreamStream struct {
	Chunks              []string `json:"chunks,omitempty"`           // raw SSE data payload lines
	ChunksTruncated     bool     `json:"chunks_truncated,omitempty"` // some chunks dropped/truncated due to limits
	AggregatedText      string   `json:"aggregated_text,omitempty"`  // best-effort aggregated assistant text
	AggregatedTruncated bool     `json:"aggregated_truncated,omitempty"`

	StreamBytes int `json:"-"`
}

type RecentCallErrorInfo struct {
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
	Status  int    `json:"status,omitempty"`
}

type RecentCallRecord struct {
	ID        uint64    `json:"id"`
	CreatedAt time.Time `json:"created_at"`

	UserID    int    `json:"user_id"`
	ChannelID int    `json:"channel_id,omitempty"`
	ModelName string `json:"model_name,omitempty"`

	Method string `json:"method"`
	Path   string `json:"path"`

	Request  RecentCallRequest           `json:"request"`
	Response *RecentCallUpstreamResponse `json:"response,omitempty"`
	Stream   *RecentCallUpstreamStream   `json:"stream,omitempty"`
	Error    *RecentCallErrorInfo        `json:"error,omitempty"`
}

type recentCallEntry struct {
	meta RecentCallRecord

	reqBodyPath   string
	respBodyPath  string
	streamPath    string
	streamAggPath string

	streamBytes  int
	streamInited bool

	mu      sync.Mutex
	evicted bool
}

type recentCallsCache struct {
	cfg RecentCallsCacheConfig

	nextID atomic.Uint64

	mu     sync.RWMutex
	buffer []*recentCallEntry

	tempSessionDir string
}

var recentCallsSingleton = newRecentCallsCache(RecentCallsCacheConfig{
	Capacity: DefaultRecentCallsCapacity,

	MaxRequestBodyBytes:  DefaultMaxRequestBodyBytes,
	MaxResponseBodyBytes: DefaultMaxResponseBodyBytes,

	MaxStreamChunkBytes: DefaultMaxStreamChunkBytes,
	MaxStreamTotalBytes: DefaultMaxStreamTotalBytes,
})

func RecentCallsCache() *recentCallsCache {
	return recentCallsSingleton
}

func newRecentCallsCache(cfg RecentCallsCacheConfig) *recentCallsCache {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultRecentCallsCapacity
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = DefaultMaxRequestBodyBytes
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		cfg.MaxResponseBodyBytes = DefaultMaxResponseBodyBytes
	}
	if cfg.MaxStreamChunkBytes <= 0 {
		cfg.MaxStreamChunkBytes = DefaultMaxStreamChunkBytes
	}
	if cfg.MaxStreamTotalBytes <= 0 {
		cfg.MaxStreamTotalBytes = DefaultMaxStreamTotalBytes
	}

	sessionDir := initRecentCallsTempDir()

	return &recentCallsCache{
		cfg:            cfg,
		buffer:         make([]*recentCallEntry, cfg.Capacity),
		tempSessionDir: sessionDir,
	}
}

func (cch *recentCallsCache) BeginFromContext(c *gin.Context, info *relaycommon.RelayInfo, rawRequestBody []byte) uint64 {
	if cch == nil || c == nil {
		return 0
	}

	id := cch.nextID.Add(1)

	path := ""
	if c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	method := ""
	if c.Request != nil {
		method = c.Request.Method
	}

	userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)

	modelName := ""
	if info != nil {
		modelName = info.OriginModelName
		if modelName == "" {
			modelName = info.UpstreamModelName
		}
	}

	rec := &RecentCallRecord{
		ID:        id,
		CreatedAt: time.Now().UTC(),

		UserID:    userID,
		ChannelID: channelID,
		ModelName: modelName,

		Method: method,
		Path:   path,

		Request: RecentCallRequest{
			Method: method,
			Path:   path,
			Header: sanitizeHeaders(c.Request.Header),
		},
	}

	bodyType, body, truncated, omitted, omitReason :=
		encodeBodyForRecord(c.Request.Header.Get("Content-Type"), rawRequestBody, cch.cfg.MaxRequestBodyBytes)
	rec.Request.BodyType = bodyType
	rec.Request.Truncated = truncated
	rec.Request.Omitted = omitted
	rec.Request.OmitReason = omitReason

	entry := &recentCallEntry{
		meta: *rec,
	}
	entry.reqBodyPath = cch.pathForID(id, "req_body.txt")
	if !omitted && body != "" {
		if entry.reqBodyPath == "" {
			entry.meta.Request.Omitted = true
			entry.meta.Request.OmitReason = "temp_dir_unavailable"
		} else if err := entry.writeTextFile(entry.reqBodyPath, body); err != nil {
			entry.meta.Request.Omitted = true
			entry.meta.Request.OmitReason = "temp_write_failed"
			entry.reqBodyPath = ""
		}
	}

	c.Set(RecentCallsContextKeyID, id)
	cch.put(entry)
	return id
}

func (cch *recentCallsCache) UpsertErrorByContext(c *gin.Context, errMsg string, errType string, errCode string, status int) {
	if cch == nil || c == nil {
		return
	}
	id := getRecentCallID(c)
	if id == 0 {
		return
	}
	cch.mu.Lock()
	entry := cch.getLocked(id)
	cch.mu.Unlock()
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.evicted {
		return
	}
	entry.meta.Error = &RecentCallErrorInfo{
		Message: errMsg,
		Type:    errType,
		Code:    errCode,
		Status:  status,
	}
}

func (cch *recentCallsCache) UpsertUpstreamResponseByContext(c *gin.Context, resp *http.Response, rawUpstreamBody []byte) {
	if cch == nil || c == nil {
		return
	}
	id := getRecentCallID(c)
	if id == 0 {
		return
	}

	header := map[string]string(nil)
	statusCode := 0
	contentType := ""
	if resp != nil {
		statusCode = resp.StatusCode
		contentType = resp.Header.Get("Content-Type")
		header = sanitizeHeaders(resp.Header)
	}

	bodyType, body, truncated, omitted, omitReason := encodeBodyForRecord(contentType, rawUpstreamBody, cch.cfg.MaxResponseBodyBytes)

	cch.mu.Lock()
	entry := cch.getLocked(id)
	cch.mu.Unlock()
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.evicted {
		return
	}

	entry.meta.Response = &RecentCallUpstreamResponse{
		StatusCode: statusCode,
		Header:     header,
		BodyType:   bodyType,
		Truncated:  truncated,
		Omitted:    omitted,
		OmitReason: omitReason,
	}

	entry.respBodyPath = cch.pathForID(id, "resp_body.txt")
	if !omitted && body != "" {
		if entry.respBodyPath == "" {
			entry.meta.Response.Omitted = true
			entry.meta.Response.OmitReason = "temp_dir_unavailable"
		} else if err := entry.writeTextFile(entry.respBodyPath, body); err != nil {
			entry.meta.Response.Omitted = true
			entry.meta.Response.OmitReason = "temp_write_failed"
			entry.respBodyPath = ""
		}
	}
}

func (cch *recentCallsCache) EnsureStreamByContext(c *gin.Context, resp *http.Response) {
	if cch == nil || c == nil {
		return
	}
	id := getRecentCallID(c)
	if id == 0 {
		return
	}

	cch.mu.Lock()
	entry := cch.getLocked(id)
	cch.mu.Unlock()
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.evicted {
		return
	}

	if entry.meta.Stream == nil {
		entry.meta.Stream = &RecentCallUpstreamStream{}
	}
	if entry.meta.Response == nil && resp != nil {
		entry.meta.Response = &RecentCallUpstreamResponse{
			StatusCode: resp.StatusCode,
			Header:     sanitizeHeaders(resp.Header),
		}
	}

	if !entry.streamInited {
		entry.streamInited = true
		entry.streamPath = cch.pathForID(id, "stream_chunks.jsonl")
		entry.streamAggPath = cch.pathForID(id, "stream_agg.txt")
		if entry.streamPath == "" || entry.ensureEmptyFile(entry.streamPath) != nil {
			entry.streamInited = false
			entry.streamPath = ""
			entry.streamAggPath = ""
			entry.meta.Stream.ChunksTruncated = true
		}
	}
}

func (cch *recentCallsCache) AppendStreamChunkByContext(c *gin.Context, chunk string) {
	if cch == nil || c == nil || chunk == "" {
		return
	}
	id := getRecentCallID(c)
	if id == 0 {
		return
	}

	chunkTruncated := false
	if cch.cfg.MaxStreamChunkBytes > 0 && len(chunk) > cch.cfg.MaxStreamChunkBytes {
		chunk = chunk[:cch.cfg.MaxStreamChunkBytes]
		chunkTruncated = true
	}

	cch.mu.Lock()
	entry := cch.getLocked(id)
	cch.mu.Unlock()
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.evicted {
		return
	}

	if chunkTruncated {
		if entry.meta.Stream == nil {
			entry.meta.Stream = &RecentCallUpstreamStream{}
		}
		entry.meta.Stream.ChunksTruncated = true
	}

	if cch.cfg.MaxStreamTotalBytes > 0 && entry.streamBytes+len(chunk) > cch.cfg.MaxStreamTotalBytes {
		if entry.meta.Stream == nil {
			entry.meta.Stream = &RecentCallUpstreamStream{}
		}
		entry.meta.Stream.ChunksTruncated = true
		return
	}

	if !entry.streamInited {
		entry.streamInited = true
		entry.streamPath = cch.pathForID(id, "stream_chunks.jsonl")
		entry.streamAggPath = cch.pathForID(id, "stream_agg.txt")
		if entry.streamPath == "" || entry.ensureEmptyFile(entry.streamPath) != nil {
			if entry.meta.Stream == nil {
				entry.meta.Stream = &RecentCallUpstreamStream{}
			}
			entry.meta.Stream.ChunksTruncated = true
			entry.streamInited = false
			entry.streamPath = ""
			entry.streamAggPath = ""
			return
		}
	}

	if err := entry.appendJSONLString(entry.streamPath, chunk); err != nil {
		if entry.meta.Stream == nil {
			entry.meta.Stream = &RecentCallUpstreamStream{}
		}
		entry.meta.Stream.ChunksTruncated = true
		return
	}
	entry.streamBytes += len(chunk)
}

func (cch *recentCallsCache) FinalizeStreamAggregatedTextByContext(c *gin.Context, aggregated string) {
	if cch == nil || c == nil {
		return
	}
	id := getRecentCallID(c)
	if id == 0 {
		return
	}

	truncated := false
	if cch.cfg.MaxResponseBodyBytes > 0 && len(aggregated) > cch.cfg.MaxResponseBodyBytes {
		aggregated = aggregated[:cch.cfg.MaxResponseBodyBytes]
		truncated = true
	}

	cch.mu.RLock()
	entry := cch.getLocked(id)
	cch.mu.RUnlock()
	if entry == nil {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.evicted {
		return
	}
	if entry.meta.Stream == nil {
		entry.meta.Stream = &RecentCallUpstreamStream{}
	}
	entry.meta.Stream.AggregatedTruncated = truncated

	if !entry.streamInited {
		entry.streamInited = true
		entry.streamPath = cch.pathForID(id, "stream_chunks.jsonl")
		entry.streamAggPath = cch.pathForID(id, "stream_agg.txt")
		if entry.streamPath == "" || entry.ensureEmptyFile(entry.streamPath) != nil {
			entry.streamInited = false
			entry.streamPath = ""
			entry.streamAggPath = ""
			entry.meta.Stream.ChunksTruncated = true
			return
		}
	}

	_ = entry.writeTextFile(entry.streamAggPath, aggregated)
}

func (cch *recentCallsCache) Get(id uint64) (*RecentCallRecord, bool) {
	if cch == nil || id == 0 {
		return nil, false
	}
	cch.mu.RLock()
	entry := cch.getLocked(id)
	cch.mu.RUnlock()
	if entry == nil {
		return nil, false
	}
	return cch.materializeEntry(entry)
}

func (cch *recentCallsCache) List(limit int, beforeID uint64) []*RecentCallRecord {
	if cch == nil {
		return nil
	}
	if limit <= 0 {
		limit = cch.cfg.Capacity
	}
	if limit > cch.cfg.Capacity {
		limit = cch.cfg.Capacity
	}

	cch.mu.RLock()
	defer cch.mu.RUnlock()

	items := make([]*recentCallEntry, 0, limit)
	for _, entry := range cch.buffer {
		if entry == nil {
			continue
		}
		if beforeID != 0 && entry.meta.ID >= beforeID {
			continue
		}
		items = append(items, entry)
	}

	sort.Slice(items, func(i, j int) bool { return items[i].meta.ID > items[j].meta.ID })
	if len(items) > limit {
		items = items[:limit]
	}

	out := make([]*RecentCallRecord, 0, len(items))
	for _, entry := range items {
		dup, ok := cch.materializeEntry(entry)
		if !ok || dup == nil {
			continue
		}
		out = append(out, dup)
	}
	return out
}

func (cch *recentCallsCache) put(entry *recentCallEntry) {
	if cch == nil || entry == nil {
		return
	}
	idx := int(entry.meta.ID % uint64(cch.cfg.Capacity))
	cch.mu.Lock()
	old := cch.buffer[idx]
	cch.buffer[idx] = entry
	cch.mu.Unlock()

	if old != nil && old.meta.ID != entry.meta.ID {
		old.mu.Lock()
		old.evicted = true
		_ = old.cleanupFiles()
		old.mu.Unlock()
	}
}

func (cch *recentCallsCache) getLocked(id uint64) *recentCallEntry {
	if cch == nil || id == 0 {
		return nil
	}
	idx := int(id % uint64(cch.cfg.Capacity))
	entry := cch.buffer[idx]
	if entry == nil || entry.meta.ID != id {
		return nil
	}
	return entry
}

func getRecentCallID(c *gin.Context) uint64 {
	if c == nil {
		return 0
	}
	v, ok := c.Get(RecentCallsContextKeyID)
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case uint64:
		return t
	case uint:
		return uint64(t)
	case int:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case int64:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case string:
		parsed, _ := strconv.ParseUint(t, 10, 64)
		return parsed
	default:
		return 0
	}
}

func sanitizeHeaders(h http.Header) map[string]string {
	if h == nil {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) == 0 {
			continue
		}
		v := strings.Join(vals, ",")
		switch strings.ToLower(k) {
		case "authorization", "x-api-key", "x-goog-api-key", "proxy-authorization":
			out[k] = "***masked***"
		default:
			out[k] = v
		}
	}
	return out
}

func encodeBodyForRecord(contentType string, body []byte, limit int) (bodyType string, encoded string, truncated bool, omitted bool, omitReason string) {
	if len(body) == 0 {
		return "unknown", "", false, true, "empty"
	}

	ct := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/") || strings.Contains(ct, "application/x-www-form-urlencoded") {
		bodyType = "text"
		if strings.HasPrefix(ct, "application/json") {
			bodyType = "json"
		}
		s := string(body)
		if limit > 0 && len(s) > limit {
			s = s[:limit]
			truncated = true
		}
		return bodyType, s, truncated, false, ""
	}

	if strings.Contains(ct, "multipart/form-data") {
		return "binary", "", false, true, "multipart_form_data"
	}

	if strings.HasPrefix(ct, "application/octet-stream") {
		// base64 with limit
		b := body
		if limit > 0 && len(b) > limit {
			b = b[:limit]
			truncated = true
		}
		return "binary", base64.StdEncoding.EncodeToString(b), truncated, false, ""
	}

	// Unknown content-type: best-effort treat as text if printable-ish, otherwise omit
	bodyType = "unknown"
	s := string(body)
	if limit > 0 && len(s) > limit {
		s = s[:limit]
		truncated = true
	}
	return bodyType, s, truncated, false, ""
}

func initRecentCallsTempDir() string {
	base := os.TempDir()
	const prefix = "new-api-recent-calls-"
	const marker = ".new-api-recent-calls"

	dir, err := os.MkdirTemp(base, prefix)
	if err != nil {
		return ""
	}
	_ = os.WriteFile(filepath.Join(dir, marker), []byte("ok\n"), 0o600)

	entries, err := os.ReadDir(base)
	if err != nil {
		return dir
	}
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		full := filepath.Join(base, name)
		if samePath(full, dir) {
			continue
		}
		if _, err := os.Stat(filepath.Join(full, marker)); err == nil {
			_ = os.RemoveAll(full)
		}
	}
	return dir
}

func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 == nil && err2 == nil {
		return strings.EqualFold(aa, bb)
	}
	return a == b
}

func (cch *recentCallsCache) pathForID(id uint64, name string) string {
	if cch == nil || id == 0 || name == "" {
		return ""
	}
	base := cch.tempSessionDir
	if base == "" {
		return ""
	}
	return filepath.Join(base, fmt.Sprintf("%d_%s", id, name))
}

func (cch *recentCallsCache) materializeEntry(entry *recentCallEntry) (*RecentCallRecord, bool) {
	if cch == nil || entry == nil {
		return nil, false
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.evicted {
		return nil, false
	}

	dup := entry.meta
	if dup.Response != nil {
		r := *dup.Response
		dup.Response = &r
	}
	if dup.Stream != nil {
		s := *dup.Stream
		s.StreamBytes = 0
		dup.Stream = &s
	}
	if dup.Error != nil {
		e := *dup.Error
		dup.Error = &e
	}

	if entry.reqBodyPath != "" && !dup.Request.Omitted {
		if body, err := os.ReadFile(entry.reqBodyPath); err == nil {
			dup.Request.Body = string(body)
		}
	}
	if entry.respBodyPath != "" && dup.Response != nil && !dup.Response.Omitted {
		if body, err := os.ReadFile(entry.respBodyPath); err == nil {
			dup.Response.Body = string(body)
		}
	}

	if entry.streamInited && dup.Stream != nil && entry.streamPath != "" {
		chunks, err := readJSONLStrings(entry.streamPath, 512<<10)
		if err == nil {
			dup.Stream.Chunks = chunks
		} else {
			dup.Stream.Chunks = []string{}
		}
	}
	if entry.streamInited && dup.Stream != nil && entry.streamAggPath != "" {
		if agg, err := os.ReadFile(entry.streamAggPath); err == nil {
			dup.Stream.AggregatedText = string(agg)
		}
	}

	return &dup, true
}

func readJSONLStrings(path string, maxLineBytes int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if maxLineBytes < 64<<10 {
		maxLineBytes = 64 << 10
	}
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)

	out := make([]string, 0, 64)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s string
		if err := common.Unmarshal([]byte(line), &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func (e *recentCallEntry) ensureEmptyFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

func (e *recentCallEntry) writeTextFile(path string, content string) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (e *recentCallEntry) appendJSONLString(path string, value string) error {
	if path == "" {
		return nil
	}
	b, err := common.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return err
	}
	if _, err := io.WriteString(f, "\n"); err != nil {
		return err
	}
	return nil
}

func (e *recentCallEntry) cleanupFiles() error {
	var firstErr error
	paths := []string{e.reqBodyPath, e.respBodyPath, e.streamPath, e.streamAggPath}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	e.reqBodyPath = ""
	e.respBodyPath = ""
	e.streamPath = ""
	e.streamAggPath = ""
	return firstErr
}
