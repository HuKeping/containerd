package server

import (
	"errors"
	"fmt"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/containerd/api/grpc/types"
	"github.com/docker/containerd/runtime"
	"github.com/docker/containerd/specs"
	"github.com/docker/containerd/supervisor"
	"golang.org/x/net/context"
)

type apiServer struct {
	sv *supervisor.Supervisor
}

// NewServer returns grpc server instance
func NewServer(sv *supervisor.Supervisor) types.APIServer {
	return &apiServer{
		sv: sv,
	}
}

func (s *apiServer) CreateContainer(ctx context.Context, c *types.CreateContainerRequest) (*types.CreateContainerResponse, error) {
	if c.BundlePath == "" {
		return nil, errors.New("empty bundle path")
	}
	e := &supervisor.StartTask{}
	e.ID = c.Id
	e.BundlePath = c.BundlePath
	e.Stdin = c.Stdin
	e.Stdout = c.Stdout
	e.Stderr = c.Stderr
	e.Labels = c.Labels
	e.NoPivotRoot = c.NoPivotRoot
	e.StartResponse = make(chan supervisor.StartResponse, 1)
	createContainerConfigCheckpoint(e, c)
	s.sv.SendTask(e)
	if err := <-e.ErrorCh(); err != nil {
		return nil, err
	}
	r := <-e.StartResponse
	apiC, err := createAPIContainer(r.Container, false)
	if err != nil {
		return nil, err
	}
	return &types.CreateContainerResponse{
		Container: apiC,
	}, nil
}

func (s *apiServer) Signal(ctx context.Context, r *types.SignalRequest) (*types.SignalResponse, error) {
	e := &supervisor.SignalTask{}
	e.ID = r.Id
	e.PID = r.Pid
	e.Signal = syscall.Signal(int(r.Signal))
	s.sv.SendTask(e)
	if err := <-e.ErrorCh(); err != nil {
		return nil, err
	}
	return &types.SignalResponse{}, nil
}

func (s *apiServer) AddProcess(ctx context.Context, r *types.AddProcessRequest) (*types.AddProcessResponse, error) {
	process := &specs.ProcessSpec{
		Terminal: r.Terminal,
		Args:     r.Args,
		Env:      r.Env,
		Cwd:      r.Cwd,
	}
	setPlatformRuntimeProcessSpecUserFields(r, process)

	if r.Id == "" {
		return nil, fmt.Errorf("container id cannot be empty")
	}
	if r.Pid == "" {
		return nil, fmt.Errorf("process id cannot be empty")
	}
	e := &supervisor.AddProcessTask{}
	e.ID = r.Id
	e.PID = r.Pid
	e.ProcessSpec = process
	e.Stdin = r.Stdin
	e.Stdout = r.Stdout
	e.Stderr = r.Stderr
	e.StartResponse = make(chan supervisor.StartResponse, 1)
	s.sv.SendTask(e)
	if err := <-e.ErrorCh(); err != nil {
		return nil, err
	}
	<-e.StartResponse
	return &types.AddProcessResponse{}, nil
}

func (s *apiServer) State(ctx context.Context, r *types.StateRequest) (*types.StateResponse, error) {
	e := &supervisor.GetContainersTask{}
	e.ID = r.Id
	s.sv.SendTask(e)
	if err := <-e.ErrorCh(); err != nil {
		return nil, err
	}
	m := s.sv.Machine()
	state := &types.StateResponse{
		Machine: &types.Machine{
			Cpus:   uint32(m.Cpus),
			Memory: uint64(m.Memory),
		},
	}
	for _, c := range e.Containers {
		apiC, err := createAPIContainer(c, true)
		if err != nil {
			return nil, err
		}
		state.Containers = append(state.Containers, apiC)
	}
	return state, nil
}

func createAPIContainer(c runtime.Container, getPids bool) (*types.Container, error) {
	processes, err := c.Processes()
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, "get processes for container: "+err.Error())
	}
	var procs []*types.Process
	for _, p := range processes {
		oldProc := p.Spec()
		stdio := p.Stdio()
		appendToProcs := &types.Process{
			Pid:       p.ID(),
			SystemPid: uint32(p.SystemPid()),
			Terminal:  oldProc.Terminal,
			Args:      oldProc.Args,
			Env:       oldProc.Env,
			Cwd:       oldProc.Cwd,
			Stdin:     stdio.Stdin,
			Stdout:    stdio.Stdout,
			Stderr:    stdio.Stderr,
		}
		setUserFieldsInProcess(appendToProcs, oldProc)
		procs = append(procs, appendToProcs)
	}
	var pids []int
	state := c.State()
	if getPids && (state == runtime.Running || state == runtime.Paused) {
		if pids, err = c.Pids(); err != nil {
			return nil, grpc.Errorf(codes.Internal, "get all pids for container: "+err.Error())
		}
	}
	return &types.Container{
		Id:         c.ID(),
		BundlePath: c.Path(),
		Processes:  procs,
		Labels:     c.Labels(),
		Status:     string(state),
		Pids:       toUint32(pids),
		Runtime:    c.Runtime(),
	}, nil
}

func toUint32(its []int) []uint32 {
	o := []uint32{}
	for _, i := range its {
		o = append(o, uint32(i))
	}
	return o
}

func (s *apiServer) UpdateContainer(ctx context.Context, r *types.UpdateContainerRequest) (*types.UpdateContainerResponse, error) {
	e := &supervisor.UpdateTask{}
	e.ID = r.Id
	e.State = runtime.State(r.Status)
	if r.Resources != nil {
		rs := r.Resources
		e.Resources = &runtime.Resource{}
		if rs.CpuShares != 0 {
			e.Resources.CPUShares = int64(rs.CpuShares)
		}
		if rs.BlkioWeight != 0 {
			e.Resources.BlkioWeight = uint16(rs.BlkioWeight)
		}
		if rs.CpuPeriod != 0 {
			e.Resources.CPUPeriod = int64(rs.CpuPeriod)
		}
		if rs.CpuQuota != 0 {
			e.Resources.CPUQuota = int64(rs.CpuQuota)
		}
		if rs.CpusetCpus != "" {
			e.Resources.CpusetCpus = rs.CpusetCpus
		}
		if rs.CpusetMems != "" {
			e.Resources.CpusetMems = rs.CpusetMems
		}
		if rs.KernelMemoryLimit != 0 {
			e.Resources.KernelMemory = int64(rs.KernelMemoryLimit)
		}
		if rs.MemoryLimit != 0 {
			e.Resources.Memory = int64(rs.MemoryLimit)
		}
		if rs.MemoryReservation != 0 {
			e.Resources.MemoryReservation = int64(rs.MemoryReservation)
		}
		if rs.MemorySwap != 0 {
			e.Resources.MemorySwap = int64(rs.MemorySwap)
		}
	}
	s.sv.SendTask(e)
	if err := <-e.ErrorCh(); err != nil {
		return nil, err
	}
	return &types.UpdateContainerResponse{}, nil
}

func (s *apiServer) UpdateProcess(ctx context.Context, r *types.UpdateProcessRequest) (*types.UpdateProcessResponse, error) {
	e := &supervisor.UpdateProcessTask{}
	e.ID = r.Id
	e.PID = r.Pid
	e.Height = int(r.Height)
	e.Width = int(r.Width)
	e.CloseStdin = r.CloseStdin
	s.sv.SendTask(e)
	if err := <-e.ErrorCh(); err != nil {
		return nil, err
	}
	return &types.UpdateProcessResponse{}, nil
}

func (s *apiServer) Events(r *types.EventsRequest, stream types.API_EventsServer) error {
	t := time.Time{}
	if r.Timestamp != 0 {
		t = time.Unix(int64(r.Timestamp), 0)
	}
	events := s.sv.Events(t)
	defer s.sv.Unsubscribe(events)
	for e := range events {
		if err := stream.Send(&types.Event{
			Id:        e.ID,
			Type:      e.Type,
			Timestamp: uint64(e.Timestamp.Unix()),
			Pid:       e.PID,
			Status:    uint32(e.Status),
		}); err != nil {
			return err
		}
	}
	return nil
}
