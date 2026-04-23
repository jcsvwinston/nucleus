package tasks

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

const (
	QueueActionPause         = "pause"
	QueueActionUnpause       = "unpause"
	QueueActionRetry         = "retry"
	QueueActionArchiveRetry  = "archive-retry"
	QueueActionRetryArchived = "retry-archived"
	QueueActionPurgeArchived = "purge-archived"
)

var supportedQueueActions = []string{
	QueueActionPause,
	QueueActionUnpause,
	QueueActionRetry,
	QueueActionArchiveRetry,
	QueueActionRetryArchived,
	QueueActionPurgeArchived,
}

// RuntimeSnapshot describes queue/worker state discoverable from Asynq runtime.
type RuntimeSnapshot struct {
	Enabled           bool                      `json:"enabled"`
	GeneratedAt       string                    `json:"generated_at"`
	Reason            string                    `json:"reason,omitempty"`
	Queues            []RuntimeQueueSnapshot    `json:"queues"`
	Schedules         []RuntimeScheduleSnapshot `json:"schedules"`
	Servers           []RuntimeServerSnapshot   `json:"servers"`
	Workers           []RuntimeWorkerSnapshot   `json:"workers"`
	TotalSchedules    int                       `json:"total_schedules"`
	TotalQueues       int                       `json:"total_queues"`
	TotalServers      int                       `json:"total_servers"`
	TotalWorkers      int                       `json:"total_workers"`
	TotalSize         int                       `json:"total_size"`
	TotalPending      int                       `json:"total_pending"`
	TotalActive       int                       `json:"total_active"`
	TotalScheduled    int                       `json:"total_scheduled"`
	TotalRetry        int                       `json:"total_retry"`
	TotalArchived     int                       `json:"total_archived"`
	TotalCompleted    int                       `json:"total_completed"`
	TotalAggregating  int                       `json:"total_aggregating"`
	TotalProcessed    int                       `json:"total_processed_today"`
	TotalFailed       int                       `json:"total_failed_today"`
	TotalProcessedAll int                       `json:"total_processed_all"`
	TotalFailedAll    int                       `json:"total_failed_all"`
}

// RuntimeQueueSnapshot holds one queue aggregate.
type RuntimeQueueSnapshot struct {
	Name           string `json:"name"`
	Paused         bool   `json:"paused"`
	LatencyMS      int64  `json:"latency_ms"`
	Size           int    `json:"size"`
	Pending        int    `json:"pending"`
	Active         int    `json:"active"`
	Scheduled      int    `json:"scheduled"`
	Retry          int    `json:"retry"`
	Archived       int    `json:"archived"`
	Completed      int    `json:"completed"`
	Aggregating    int    `json:"aggregating"`
	ProcessedToday int    `json:"processed_today"`
	FailedToday    int    `json:"failed_today"`
	ProcessedAll   int    `json:"processed_all"`
	FailedAll      int    `json:"failed_all"`
}

// RuntimeScheduleSnapshot holds one registered periodic task entry.
type RuntimeScheduleSnapshot struct {
	ID            string `json:"id"`
	Spec          string `json:"spec"`
	TaskType      string `json:"task_type"`
	NextEnqueueAt string `json:"next_enqueue_at,omitempty"`
	PrevEnqueueAt string `json:"prev_enqueue_at,omitempty"`
}

// RuntimeServerSnapshot holds one server aggregate.
type RuntimeServerSnapshot struct {
	ID             string         `json:"id"`
	Host           string         `json:"host"`
	PID            int            `json:"pid"`
	Status         string         `json:"status"`
	StartedAt      string         `json:"started_at,omitempty"`
	Concurrency    int            `json:"concurrency"`
	StrictPriority bool           `json:"strict_priority"`
	Queues         map[string]int `json:"queues"`
	ActiveWorkers  int            `json:"active_workers"`
}

// RuntimeWorkerSnapshot describes one active worker task.
type RuntimeWorkerSnapshot struct {
	ServerID  string `json:"server_id"`
	Host      string `json:"host"`
	PID       int    `json:"pid"`
	Queue     string `json:"queue"`
	TaskID    string `json:"task_id"`
	TaskType  string `json:"task_type"`
	StartedAt string `json:"started_at,omitempty"`
	Deadline  string `json:"deadline,omitempty"`
}

// QueueActionResult is the result of one operational queue action.
type QueueActionResult struct {
	Enabled     bool   `json:"enabled"`
	GeneratedAt string `json:"generated_at"`
	Queue       string `json:"queue"`
	Action      string `json:"action"`
	Applied     bool   `json:"applied"`
	Affected    int    `json:"affected,omitempty"`
	Message     string `json:"message,omitempty"`
}

// InspectRuntime returns a non-persistent runtime snapshot from Redis/Asynq.
// It never panics; when unavailable it returns Enabled=false with a reason.
func InspectRuntime(redisURL string) RuntimeSnapshot {
	now := time.Now().UTC()
	out := RuntimeSnapshot{
		Enabled:     false,
		GeneratedAt: now.Format(time.RFC3339),
		Queues:      []RuntimeQueueSnapshot{},
		Schedules:   []RuntimeScheduleSnapshot{},
		Servers:     []RuntimeServerSnapshot{},
		Workers:     []RuntimeWorkerSnapshot{},
	}

	trimmed := strings.TrimSpace(redisURL)
	if trimmed == "" {
		out.Reason = "redis_url is not configured"
		return out
	}

	redisOpts, err := redisClientOptFromURL(trimmed)
	if err != nil {
		out.Reason = err.Error()
		return out
	}

	inspector := asynq.NewInspector(redisOpts)
	defer inspector.Close()

	queueNames, err := inspector.Queues()
	if err != nil {
		out.Reason = err.Error()
		return out
	}
	sort.Strings(queueNames)

	for _, queue := range queueNames {
		info, infoErr := inspector.GetQueueInfo(queue)
		if infoErr != nil || info == nil {
			continue
		}
		row := RuntimeQueueSnapshot{
			Name:           info.Queue,
			Paused:         info.Paused,
			LatencyMS:      int64(info.Latency / time.Millisecond),
			Size:           info.Size,
			Pending:        info.Pending,
			Active:         info.Active,
			Scheduled:      info.Scheduled,
			Retry:          info.Retry,
			Archived:       info.Archived,
			Completed:      info.Completed,
			Aggregating:    info.Aggregating,
			ProcessedToday: info.Processed,
			FailedToday:    info.Failed,
			ProcessedAll:   info.ProcessedTotal,
			FailedAll:      info.FailedTotal,
		}
		out.Queues = append(out.Queues, row)
		out.TotalSize += row.Size
		out.TotalPending += row.Pending
		out.TotalActive += row.Active
		out.TotalScheduled += row.Scheduled
		out.TotalRetry += row.Retry
		out.TotalArchived += row.Archived
		out.TotalCompleted += row.Completed
		out.TotalAggregating += row.Aggregating
		out.TotalProcessed += row.ProcessedToday
		out.TotalFailed += row.FailedToday
		out.TotalProcessedAll += row.ProcessedAll
		out.TotalFailedAll += row.FailedAll
	}

	scheduleRows, err := inspector.SchedulerEntries()
	if err == nil {
		for _, entry := range scheduleRows {
			if entry == nil {
				continue
			}
			next := ""
			if !entry.Next.IsZero() {
				next = entry.Next.UTC().Format(time.RFC3339)
			}
			prev := ""
			if !entry.Prev.IsZero() {
				prev = entry.Prev.UTC().Format(time.RFC3339)
			}
			taskType := ""
			if entry.Task != nil {
				taskType = entry.Task.Type()
			}
			out.Schedules = append(out.Schedules, RuntimeScheduleSnapshot{
				ID:            entry.ID,
				Spec:          entry.Spec,
				TaskType:      taskType,
				NextEnqueueAt: next,
				PrevEnqueueAt: prev,
			})
		}
		sort.SliceStable(out.Schedules, func(i, j int) bool {
			if out.Schedules[i].Spec != out.Schedules[j].Spec {
				return out.Schedules[i].Spec < out.Schedules[j].Spec
			}
			if out.Schedules[i].TaskType != out.Schedules[j].TaskType {
				return out.Schedules[i].TaskType < out.Schedules[j].TaskType
			}
			return out.Schedules[i].ID < out.Schedules[j].ID
		})
	}

	serverRows, err := inspector.Servers()
	if err != nil {
		// Queue snapshot is still useful even if server snapshot fails.
		out.Enabled = len(out.Queues) > 0 || len(out.Schedules) > 0
		out.TotalSchedules = len(out.Schedules)
		out.TotalQueues = len(out.Queues)
		out.Reason = err.Error()
		return out
	}
	sort.SliceStable(serverRows, func(i, j int) bool {
		if serverRows[i].Host != serverRows[j].Host {
			return serverRows[i].Host < serverRows[j].Host
		}
		return serverRows[i].ID < serverRows[j].ID
	})

	for _, server := range serverRows {
		if server == nil {
			continue
		}
		started := ""
		if !server.Started.IsZero() {
			started = server.Started.UTC().Format(time.RFC3339)
		}
		queues := map[string]int{}
		for key, val := range server.Queues {
			queues[key] = val
		}

		serverSnapshot := RuntimeServerSnapshot{
			ID:             server.ID,
			Host:           server.Host,
			PID:            server.PID,
			Status:         server.Status,
			StartedAt:      started,
			Concurrency:    server.Concurrency,
			StrictPriority: server.StrictPriority,
			Queues:         queues,
			ActiveWorkers:  len(server.ActiveWorkers),
		}
		out.Servers = append(out.Servers, serverSnapshot)

		for _, worker := range server.ActiveWorkers {
			if worker == nil {
				continue
			}
			startedAt := ""
			if !worker.Started.IsZero() {
				startedAt = worker.Started.UTC().Format(time.RFC3339)
			}
			deadline := ""
			if !worker.Deadline.IsZero() {
				deadline = worker.Deadline.UTC().Format(time.RFC3339)
			}
			out.Workers = append(out.Workers, RuntimeWorkerSnapshot{
				ServerID:  server.ID,
				Host:      server.Host,
				PID:       server.PID,
				Queue:     worker.Queue,
				TaskID:    worker.TaskID,
				TaskType:  worker.TaskType,
				StartedAt: startedAt,
				Deadline:  deadline,
			})
		}
	}
	sort.SliceStable(out.Workers, func(i, j int) bool {
		if out.Workers[i].Queue != out.Workers[j].Queue {
			return out.Workers[i].Queue < out.Workers[j].Queue
		}
		if out.Workers[i].TaskType != out.Workers[j].TaskType {
			return out.Workers[i].TaskType < out.Workers[j].TaskType
		}
		return out.Workers[i].TaskID < out.Workers[j].TaskID
	})

	out.Enabled = len(out.Queues) > 0 || len(out.Schedules) > 0 || len(out.Servers) > 0
	if !out.Enabled && out.Reason == "" {
		out.Reason = "no queues or workers discovered"
	}
	out.TotalSchedules = len(out.Schedules)
	out.TotalQueues = len(out.Queues)
	out.TotalServers = len(out.Servers)
	out.TotalWorkers = len(out.Workers)
	return out
}

// OperateQueue executes one operational queue action via Asynq inspector.
// Supported actions:
// - pause
// - unpause
// - retry (run all retry tasks in queue)
// - archive-retry (move retry tasks to archived/dead-letter)
// - retry-archived (move archived/dead-letter tasks back to pending)
// - purge-archived (delete archived/dead-letter tasks)
func OperateQueue(redisURL, queue, action string) (QueueActionResult, error) {
	now := time.Now().UTC()
	normalizedAction, ok := NormalizeQueueAction(action)
	out := QueueActionResult{
		Enabled:     false,
		GeneratedAt: now.Format(time.RFC3339),
		Queue:       strings.TrimSpace(queue),
		Action:      normalizedAction,
		Applied:     false,
	}

	trimmed := strings.TrimSpace(redisURL)
	if trimmed == "" {
		return out, ErrRedisURLRequired
	}
	if out.Queue == "" {
		return out, fmt.Errorf("queue is required")
	}
	if !ok {
		return out, fmt.Errorf("unsupported queue action %q", out.Action)
	}

	redisOpts, err := redisClientOptFromURL(trimmed)
	if err != nil {
		return out, err
	}
	inspector := asynq.NewInspector(redisOpts)
	defer inspector.Close()

	switch out.Action {
	case QueueActionPause:
		if err := inspector.PauseQueue(out.Queue); err != nil {
			return out, err
		}
		out.Enabled = true
		out.Applied = true
		out.Message = "queue paused"
		return out, nil
	case QueueActionUnpause:
		if err := inspector.UnpauseQueue(out.Queue); err != nil {
			return out, err
		}
		out.Enabled = true
		out.Applied = true
		out.Message = "queue resumed"
		return out, nil
	case QueueActionRetry:
		count, err := inspector.RunAllRetryTasks(out.Queue)
		if err != nil {
			return out, err
		}
		out.Enabled = true
		out.Applied = true
		out.Affected = count
		out.Message = "retry tasks moved to pending"
		return out, nil
	case QueueActionArchiveRetry:
		count, err := inspector.ArchiveAllRetryTasks(out.Queue)
		if err != nil {
			return out, err
		}
		out.Enabled = true
		out.Applied = true
		out.Affected = count
		out.Message = "retry tasks moved to archived"
		return out, nil
	case QueueActionRetryArchived:
		count, err := inspector.RunAllArchivedTasks(out.Queue)
		if err != nil {
			return out, err
		}
		out.Enabled = true
		out.Applied = true
		out.Affected = count
		out.Message = "archived tasks moved to pending"
		return out, nil
	case QueueActionPurgeArchived:
		count, err := inspector.DeleteAllArchivedTasks(out.Queue)
		if err != nil {
			return out, err
		}
		out.Enabled = true
		out.Applied = true
		out.Affected = count
		out.Message = "archived tasks deleted"
		return out, nil
	default:
		return out, fmt.Errorf("unsupported queue action %q", out.Action)
	}
}

func NormalizeQueueAction(raw string) (string, bool) {
	action := strings.ToLower(strings.TrimSpace(raw))
	for _, candidate := range supportedQueueActions {
		if action == candidate {
			return action, true
		}
	}
	return action, false
}

func SupportedQueueActions() []string {
	out := make([]string, len(supportedQueueActions))
	copy(out, supportedQueueActions)
	return out
}
