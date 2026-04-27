package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/gluon/internal/model"
)

const (
	GluonDir     = ".gluon"
	PlansDir     = "plans"
	ExecDir      = "executions"
	LogsDir      = "logs"
	LatestName   = "latest"
	StateName    = "state.json"
	ManifestName = "manifest.json"
	MetadataName = "metadata.json"
)

type ExecState struct {
	ExecID       string               `json:"execId"`
	PlanChecksum string               `json:"planChecksum"`
	Jobs         map[string]*JobState `json:"jobs"`
}

type JobState struct {
	Status     string            `json:"status"`
	StartedAt  string            `json:"startedAt,omitempty"`
	FinishedAt string            `json:"finishedAt,omitempty"`
	Steps      map[string]string `json:"steps"`
	LastError  string            `json:"lastError,omitempty"`
}

type ExecutionLink struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	JobID  string `json:"jobId,omitempty"`
	StepID string `json:"stepId,omitempty"`
}

type ExecMetadata struct {
	ExecID     string          `json:"execId"`
	PlanID     string          `json:"planId"`
	PlanName   string          `json:"planName"`
	StartedAt  string          `json:"startedAt"`
	FinishedAt string          `json:"finishedAt,omitempty"`
	Status     string          `json:"status"`
	Trigger    string          `json:"trigger"`
	User       string          `json:"user"`
	DryRun     bool            `json:"dryRun"`
	JobTotal   int             `json:"jobTotal"`
	JobDone    int             `json:"jobDone"`
	JobFailed  int             `json:"jobFailed"`
	Links      []ExecutionLink `json:"links,omitempty"`
}

type Store struct {
	BaseDir string
}

func NewStore(workDir string) *Store {
	return &Store{BaseDir: filepath.Join(workDir, GluonDir)}
}

func (s *Store) EnsureDirs() error {
	dirs := []string{
		filepath.Join(s.BaseDir, PlansDir),
		filepath.Join(s.BaseDir, ExecDir),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// --- Plan Store ---

func (s *Store) PlanDir() string {
	return filepath.Join(s.BaseDir, PlansDir)
}

func (s *Store) SavePlan(plan *model.Plan, name string) error {
	if err := s.EnsureDirs(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize plan: %w", err)
	}

	checksum := planChecksumShort(plan)

	checksumPath := filepath.Join(s.PlanDir(), checksum+".json")
	if err := os.WriteFile(checksumPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write plan %s: %w", checksumPath, err)
	}

	latestPath := filepath.Join(s.PlanDir(), LatestName+".json")
	if err := os.WriteFile(latestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write latest plan: %w", err)
	}

	if name != "" {
		namedPath := filepath.Join(s.PlanDir(), name+".json")
		if err := os.WriteFile(namedPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write named plan %s: %w", namedPath, err)
		}
	}

	return nil
}

func (s *Store) ResolvePlanRef(ref string) (string, error) {
	if ref == "" || ref == LatestName {
		path := filepath.Join(s.PlanDir(), LatestName+".json")
		if fileExists(path) {
			return path, nil
		}
		return "", fmt.Errorf("no plan found; run 'gluon plan' first")
	}

	if fileExists(ref) {
		return ref, nil
	}

	namedPath := filepath.Join(s.PlanDir(), ref+".json")
	if fileExists(namedPath) {
		return namedPath, nil
	}

	entries, err := os.ReadDir(s.PlanDir())
	if err == nil {
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".json")
			if strings.HasPrefix(name, ref) && name != LatestName {
				return filepath.Join(s.PlanDir(), entry.Name()), nil
			}
		}
	}

	return "", fmt.Errorf("plan not found: %s", ref)
}

func (s *Store) ListPlans() ([]PlanEntry, error) {
	dir := s.PlanDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var plans []PlanEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		info, _ := entry.Info()
		var age time.Time
		if info != nil {
			age = info.ModTime()
		}

		path := filepath.Join(dir, entry.Name())
		plan, _ := LoadPlanFile(path)
		jobCount := 0
		checksum := ""
		if plan != nil {
			jobCount = len(plan.Jobs)
			checksum = planChecksumShort(plan)
		}

		plans = append(plans, PlanEntry{
			Name:      name,
			Path:      path,
			Checksum:  checksum,
			Jobs:      jobCount,
			CreatedAt: age,
		})
	}
	return plans, nil
}

type PlanEntry struct {
	Name      string
	Path      string
	Checksum  string
	Jobs      int
	CreatedAt time.Time
}

// --- Execution Store ---

func (s *Store) ExecDir() string {
	return filepath.Join(s.BaseDir, ExecDir)
}

func GenerateExecID(planName string) string {
	date := time.Now().Format("20060102")
	randBytes := make([]byte, 3)
	rand.Read(randBytes)
	suffix := hex.EncodeToString(randBytes)

	name := strings.ReplaceAll(planName, " ", "-")
	name = strings.ToLower(name)
	if name == "" {
		name = "run"
	}
	if len(name) > 30 {
		name = name[:30]
	}
	return fmt.Sprintf("%s-%s-%s", name, date, suffix)
}

func (s *Store) CreateExecution(execID string, plan *model.Plan) (string, error) {
	if err := s.EnsureDirs(); err != nil {
		return "", err
	}

	execPath := filepath.Join(s.ExecDir(), execID)
	if err := os.MkdirAll(execPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create execution directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(execPath, LogsDir), 0755); err != nil {
		return "", fmt.Errorf("failed to create logs directory: %w", err)
	}

	if plan != nil {
		manifestData, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to serialize manifest: %w", err)
		}
		if err := os.WriteFile(filepath.Join(execPath, ManifestName), manifestData, 0644); err != nil {
			return "", fmt.Errorf("failed to write manifest: %w", err)
		}
	}

	latestLink := filepath.Join(s.ExecDir(), LatestName)
	os.Remove(latestLink)
	os.Symlink(execID, latestLink)

	return execPath, nil
}

func (s *Store) ExecPath(execID string) string {
	return filepath.Join(s.ExecDir(), execID)
}

func (s *Store) StatePath(execID string) string {
	return filepath.Join(s.ExecDir(), execID, StateName)
}

func (s *Store) MetadataPath(execID string) string {
	return filepath.Join(s.ExecDir(), execID, MetadataName)
}

func (s *Store) LogDir(execID, jobID string) string {
	return filepath.Join(s.ExecDir(), execID, LogsDir, sanitizePathSegment(jobID))
}

func (s *Store) LogPath(execID, jobID, stepID string) string {
	return filepath.Join(s.LogDir(execID, jobID), sanitizePathSegment(stepID)+".log")
}

func (s *Store) LoadState(execID string) (*ExecState, error) {
	path := s.StatePath(execID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ExecState{
				ExecID: execID,
				Jobs:   map[string]*JobState{},
			}, nil
		}
		return nil, fmt.Errorf("failed to read state file %s: %w", path, err)
	}

	var st ExecState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to parse state file %s: %w", path, err)
	}
	if st.Jobs == nil {
		st.Jobs = map[string]*JobState{}
	}
	st.ExecID = execID
	return &st, nil
}

func (s *Store) SaveState(execID string, st *ExecState) error {
	path := s.StatePath(execID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to replace state file: %w", err)
	}
	return nil
}

func (s *Store) SaveMetadata(execID string, meta *ExecMetadata) error {
	path := s.MetadataPath(execID)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func (s *Store) LoadMetadata(execID string) (*ExecMetadata, error) {
	path := s.MetadataPath(execID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var meta ExecMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Store) ListExecutions() ([]ExecEntry, error) {
	dir := s.ExecDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var execs []ExecEntry
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == LatestName {
			continue
		}
		execID := entry.Name()
		meta, _ := s.LoadMetadata(execID)
		st, _ := s.LoadState(execID)
		info, _ := entry.Info()

		e := ExecEntry{ID: execID}
		if meta != nil {
			e.PlanName = meta.PlanName
			e.Status = meta.Status
			e.StartedAt = meta.StartedAt
			e.FinishedAt = meta.FinishedAt
			e.JobTotal = meta.JobTotal
			e.JobDone = meta.JobDone
			e.JobFailed = meta.JobFailed
		} else if info != nil {
			e.StartedAt = info.ModTime().Format(time.RFC3339)
			e.Status = "unknown"
		}
		if counts := SummarizeExecutionState(st); counts.Total > 0 {
			e.JobTotal = counts.Total
			e.JobDone = counts.Completed
			e.JobFailed = counts.Failed
		}

		execs = append(execs, e)
	}

	sort.Slice(execs, func(i, j int) bool {
		return execs[i].StartedAt > execs[j].StartedAt
	})

	return execs, nil
}

func (s *Store) ResolveExecID(ref string) (string, error) {
	if ref == "" || ref == LatestName {
		target, err := os.Readlink(filepath.Join(s.ExecDir(), LatestName))
		if err != nil {
			execs, listErr := s.ListExecutions()
			if listErr != nil || len(execs) == 0 {
				return "", fmt.Errorf("no executions found")
			}
			return execs[0].ID, nil
		}
		return target, nil
	}

	if dirExists(filepath.Join(s.ExecDir(), ref)) {
		return ref, nil
	}

	entries, err := os.ReadDir(s.ExecDir())
	if err != nil {
		return "", fmt.Errorf("no executions found")
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ref) {
			return entry.Name(), nil
		}
	}

	return "", fmt.Errorf("execution not found: %s", ref)
}

type ExecEntry struct {
	ID         string
	PlanName   string
	Status     string
	StartedAt  string
	FinishedAt string
	JobTotal   int
	JobDone    int
	JobFailed  int
}

type ExecutionCounts struct {
	Total     int
	Completed int
	Failed    int
	Running   int
	Pending   int
}

func SummarizeExecutionState(st *ExecState) ExecutionCounts {
	if st == nil {
		return ExecutionCounts{}
	}

	counts := ExecutionCounts{}
	for _, job := range st.Jobs {
		if job == nil {
			continue
		}
		counts.Total++
		switch strings.ToLower(strings.TrimSpace(job.Status)) {
		case "completed":
			counts.Completed++
		case "failed":
			counts.Failed++
		case "running":
			counts.Running++
		default:
			counts.Pending++
		}
	}
	return counts
}

// --- GC ---

func (s *Store) GC(maxCount int, maxAgeDays int, dryRun bool) ([]string, error) {
	execs, err := s.ListExecutions()
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	var toRemove []string

	for i, exec := range execs {
		if i < maxCount {
			t, parseErr := time.Parse(time.RFC3339, exec.StartedAt)
			if parseErr != nil || t.After(cutoff) {
				continue
			}
		}
		toRemove = append(toRemove, exec.ID)
	}

	if !dryRun {
		for _, id := range toRemove {
			os.RemoveAll(filepath.Join(s.ExecDir(), id))
		}
		orphanPlans, _ := s.gcOrphanPlans()
		toRemove = append(toRemove, orphanPlans...)
	}

	return toRemove, nil
}

func (s *Store) gcOrphanPlans() ([]string, error) {
	dir := s.PlanDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var removed []string
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if name == LatestName || !looksLikeChecksum(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > 30*24*time.Hour {
			os.Remove(filepath.Join(dir, entry.Name()))
			removed = append(removed, name)
		}
	}
	return removed, nil
}

// --- Legacy migration ---

func (s *Store) MigrateLegacyState(workDir string) (bool, error) {
	legacy := filepath.Join(workDir, ".gluon-state.json")
	if !fileExists(legacy) {
		return false, nil
	}

	if err := s.EnsureDirs(); err != nil {
		return false, err
	}

	execID := "migrated-" + time.Now().Format("20060102-150405")
	execPath := filepath.Join(s.ExecDir(), execID)
	if err := os.MkdirAll(execPath, 0755); err != nil {
		return false, err
	}

	data, err := os.ReadFile(legacy)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(execPath, StateName), data, 0644); err != nil {
		return false, err
	}

	os.Remove(legacy)

	latestLink := filepath.Join(s.ExecDir(), LatestName)
	os.Remove(latestLink)
	os.Symlink(execID, latestLink)

	return true, nil
}

// --- Helpers ---

func planChecksumShort(plan *model.Plan) string {
	if plan == nil || plan.Metadata.Checksum == "" {
		return ""
	}
	cs := strings.TrimPrefix(plan.Metadata.Checksum, "sha256-")
	if len(cs) > 12 {
		return cs[:12]
	}
	return cs
}

func PlanChecksumShort(plan *model.Plan) string {
	return planChecksumShort(plan)
}

func sanitizePathSegment(s string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(s)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func looksLikeChecksum(name string) bool {
	if len(name) < 8 {
		return false
	}
	for _, c := range name {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func LoadPlanFile(path string) (*model.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan model.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}
