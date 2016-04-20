package supervisor

import (
	"fmt"
	"time"

	"github.com/docker/containerd/runtime"
	"github.com/docker/containerd/specs"
)

type AddProcessTask struct {
	baseTask
	ID            string
	PID           string
	Runtime       string
	Stdout        string
	Stderr        string
	Stdin         string
	ProcessSpec   *specs.ProcessSpec
	StartResponse chan StartResponse
}

func (s *Supervisor) addProcess(t *AddProcessTask) error {
	start := time.Now()
	ci, ok := s.containers[t.ID]
	if !ok {
		return ErrContainerNotFound
	}

	// Use the default runtime from supervisor(the daemon) if the one
	// for client was not specified
	if t.Runtime == "" {
		t.Runtime = s.runtime
	}

	// The runtime of the new client command should be the same as
	// the one we use when starting the container.
	if ci.container.Runtime() != t.Runtime {
		return fmt.Errorf("Expect runtime:%s, got:%s", ci.container.Runtime(), t.Runtime)
	}

	process, err := ci.container.Exec(t.PID, *t.ProcessSpec, runtime.NewStdio(t.Stdin, t.Stdout, t.Stderr))
	if err != nil {
		return err
	}
	if err := s.monitorProcess(process); err != nil {
		return err
	}
	ExecProcessTimer.UpdateSince(start)
	t.StartResponse <- StartResponse{}
	s.notifySubscribers(Event{
		Timestamp: time.Now(),
		Type:      StateStartProcess,
		PID:       t.PID,
		ID:        t.ID,
	})
	return nil
}
