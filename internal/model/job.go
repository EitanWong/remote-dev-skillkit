package model

import "time"

type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCanceled  JobStatus = "canceled"
)

type Job struct {
	ID        string         `json:"id"`
	HostID    string         `json:"host_id"`
	Adapter   string         `json:"adapter"`
	Intent    string         `json:"intent"`
	Policy    map[string]any `json:"policy"`
	Status    JobStatus      `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	StartedAt *time.Time     `json:"started_at,omitempty"`
	EndedAt   *time.Time     `json:"ended_at,omitempty"`
}

type Artifact struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func NewJob(hostID, adapter, intent string, policy map[string]any, now time.Time) (Job, error) {
	id, err := newID("job")
	if err != nil {
		return Job{}, err
	}
	return Job{
		ID:        id,
		HostID:    hostID,
		Adapter:   adapter,
		Intent:    intent,
		Policy:    policy,
		Status:    JobStatusQueued,
		CreatedAt: now.UTC(),
	}, nil
}

func NewArtifact(jobID, kind, name, content string, now time.Time) (Artifact, error) {
	id, err := newID("art")
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{
		ID:        id,
		JobID:     jobID,
		Kind:      kind,
		Name:      name,
		Content:   content,
		CreatedAt: now.UTC(),
	}, nil
}
