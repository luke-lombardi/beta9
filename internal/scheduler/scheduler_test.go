package scheduler

import (
	"context"
	"errors"
	"log"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/beam-cloud/beam/internal/common"
	repo "github.com/beam-cloud/beam/internal/repository"
	"github.com/beam-cloud/beam/internal/types"
	"github.com/google/uuid"
	"github.com/tj/assert"
)

func NewschedulerForTest() (*Scheduler, error) {
	s, err := miniredis.Run()
	if err != nil {
		return nil, err
	}

	rdb, err := common.NewRedisClient(common.WithAddress(s.Addr()))
	if err != nil {
		return nil, err
	}

	eventBus := common.NewEventBus(rdb)
	workerRepo := repo.NewWorkerRedisRepositoryForTest(rdb)
	workerPoolRepo := repo.NewWorkerPoolRedisRepository(rdb)
	containerRepo := repo.NewContainerRedisRepositoryForTest(rdb)
	requestBacklog := NewRequestBacklogForTest(rdb)

	workerPoolConfig := &WorkerPoolConfig{
		DataVolumeName:             "beam-data-volume-name",
		DefaultWorkerCpuRequest:    1000,
		DefaultWorkerMemoryRequest: 1000,
		DefaultMaxGpuCpuRequest:    1000,
		DefaultMaxGpuMemoryRequest: 1000,
	}

	factory := WorkerPoolControllerFactoryForTest()
	factoryConfig := &WorkerPoolControllerConfigForTest{
		namespace:        "beam-namespace",
		workerPoolConfig: workerPoolConfig,
		workerRepo:       workerRepo,
	}

	workerPoolManager := NewWorkerPoolManager(workerPoolRepo)
	workerPoolResources := []types.WorkerPoolResource{
		types.NewWorkerPoolResource("beam-cpu"),
		types.NewWorkerPoolResource("beam-a10g"),
		types.NewWorkerPoolResource("beam-t4"),
	}
	workerPoolManager.LoadPools(factory, factoryConfig, workerPoolResources)

	return &Scheduler{
		eventBus:          eventBus,
		workerRepo:        workerRepo,
		workerPoolManager: workerPoolManager,
		metricsRepo:       repo.NewMetricsStatsdRepositoryForTest(),
		requestBacklog:    requestBacklog,
		ContainerRepo:     containerRepo,
	}, nil
}

func WorkerPoolControllerFactoryForTest() WorkerPoolControllerFactory {
	return func(resource *types.WorkerPoolResource, config WorkerPoolControllerConfig) (WorkerPoolController, error) {
		if c, ok := config.(*WorkerPoolControllerConfigForTest); ok {
			return NewWorkerPoolControllerForTest(resource.Name, c)
		}

		return nil, errors.New("test worker pool controller factory received invalid config")
	}
}

type WorkerPoolControllerConfigForTest struct {
	namespace        string
	workerPoolConfig *WorkerPoolConfig
	workerRepo       repo.WorkerRepository
}

type WorkerPoolControllerForTest struct {
	name   string
	config *WorkerPoolControllerConfigForTest
}

func NewWorkerPoolControllerForTest(workerPoolName string, config *WorkerPoolControllerConfigForTest) (WorkerPoolController, error) {
	return &WorkerPoolControllerForTest{
		name:   workerPoolName,
		config: config,
	}, nil
}

func (wpc *WorkerPoolControllerForTest) generateWorkerId() string {
	return uuid.New().String()[:8]
}

func (wpc *WorkerPoolControllerForTest) AddWorkerWithId(workerId string, cpu int64, memory int64, gpuType string) (*types.Worker, error) {
	// TODO: implement and add test
	return nil, nil
}

func (wpc *WorkerPoolControllerForTest) AddWorker(cpu int64, memory int64, gpuType string) (*types.Worker, error) {
	workerId := wpc.generateWorkerId()
	worker := &types.Worker{
		Id:     workerId,
		Cpu:    cpu,
		Memory: memory,
		Gpu:    gpuType,
		Status: types.WorkerStatusPending,
	}

	// Add the worker state
	err := wpc.config.workerRepo.AddWorker(worker)
	if err != nil {
		log.Printf("Unable to create worker: %+v\n", err)
		return nil, err
	}

	return worker, nil
}

func (wpc *WorkerPoolControllerForTest) Name() string {
	return wpc.name
}

func (wpc *WorkerPoolControllerForTest) FreeCapacity() (*WorkerPoolCapacity, error) {
	return &WorkerPoolCapacity{}, nil
}

func TestNewschedulerForTest(t *testing.T) {
	wb, err := NewschedulerForTest()
	assert.Nil(t, err)
	assert.NotNil(t, wb)
}

func TestRunContainer(t *testing.T) {
	wb, err := NewschedulerForTest()
	assert.Nil(t, err)
	assert.NotNil(t, wb)

	// Schedule a container
	err = wb.Run(&types.ContainerRequest{
		ContainerId: "test-container",
	})
	assert.Nil(t, err)

	// Make sure you can't schedule a container with the same ID twice
	err = wb.Run(&types.ContainerRequest{
		ContainerId: "test-container",
	})

	if err != nil {
		_, ok := err.(*types.ContainerAlreadyScheduledError)
		assert.True(t, ok, "error is not of type *types.ContainerAlreadyScheduledError")
	} else {
		t.Error("Expected error, but got nil")
	}
}

func TestProcessRequests(t *testing.T) {
	wb, err := NewschedulerForTest()
	assert.Nil(t, err)
	assert.NotNil(t, wb)

	// Prepare some requests to process.
	requests := []*types.ContainerRequest{
		{
			ContainerId: uuid.New().String(),
			Cpu:         1000,
			Memory:      2000,
			Gpu:         "A10G",
		},
		{
			ContainerId: uuid.New().String(),
			Cpu:         1000,
			Memory:      2000,
			Gpu:         "T4",
		},
		{
			ContainerId: uuid.New().String(),
			Cpu:         1000,
			Memory:      2000,
			Gpu:         "",
		},
	}

	for _, req := range requests {
		err = wb.Run(req)
		if err != nil {
			t.Errorf("Unexpected error while adding request to backlog: %s", err)
		}
	}

	assert.Equal(t, int64(3), wb.requestBacklog.Len())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				wb.processRequests()
			}
		}
	}()

	<-ctx.Done()

	assert.Equal(t, int64(0), wb.requestBacklog.Len())
}

func TestGetController(t *testing.T) {
	t.Run("returns correct controller", func(t *testing.T) {
		wb, _ := NewschedulerForTest()

		cpuRequest := &types.ContainerRequest{Gpu: ""}
		cpuController, err := wb.getController(cpuRequest)
		if err != nil || cpuController.Name() != "beam-cpu" {
			t.Errorf("Expected beam-cpu controller, got %v, error: %v", cpuController, err)
		}

		a10gRequest := &types.ContainerRequest{Gpu: "A10G"}
		a10gController, err := wb.getController(a10gRequest)
		if err != nil || a10gController.Name() != "beam-a10g" {
			t.Errorf("Expected beam-a10g controller, got %v, error: %v", a10gController, err)
		}

		t4Request := &types.ContainerRequest{Gpu: "T4"}
		t4Controller, err := wb.getController(t4Request)
		if err != nil || t4Controller.Name() != "beam-t4" {
			t.Errorf("Expected beam-t4 controller, got %v, error: %v", t4Controller, err)
		}
	})

	t.Run("returns error if no suitable controller found", func(t *testing.T) {
		wb, _ := NewschedulerForTest()

		unknownRequest := &types.ContainerRequest{Gpu: "UNKNOWN_GPU"}
		_, err := wb.getController(unknownRequest)
		if err == nil {
			t.Errorf("Expected error for unknown GPU type, got nil")
		}
	})
}

func TestSelectGPUWorker(t *testing.T) {
	wb, err := NewschedulerForTest()
	assert.Nil(t, err)
	assert.NotNil(t, wb)

	newWorker := &types.Worker{
		Status: types.WorkerStatusPending,
		Cpu:    1000,
		Memory: 1000,
		Gpu:    "A10G",
	}

	// Create a new worker
	err = wb.workerRepo.AddWorker(newWorker)
	assert.Nil(t, err)

	firstRequest := &types.ContainerRequest{
		Cpu:    1000,
		Memory: 1000,
		Gpu:    "A10G",
	}

	secondRequest := &types.ContainerRequest{
		Cpu:    1000,
		Memory: 1000,
		Gpu:    "A10G",
	}

	// Select a worker for the request
	worker, err := wb.selectWorker(firstRequest)
	assert.Nil(t, err)

	// Check if the worker selected has the "A10G" GPU
	assert.Equal(t, newWorker.Gpu, worker.Gpu)
	assert.Equal(t, newWorker.Id, worker.Id)

	// Actually schedule the request
	err = wb.scheduleRequest(worker, firstRequest)
	assert.Nil(t, err)

	// We have no workers left, so this one should fail
	_, err = wb.selectWorker(secondRequest)
	assert.Error(t, err)

	_, ok := err.(*types.ErrNoSuitableWorkerFound)
	assert.True(t, ok)
}

func TestSelectCPUWorker(t *testing.T) {
	wb, err := NewschedulerForTest()
	assert.Nil(t, err)
	assert.NotNil(t, wb)

	newWorker := &types.Worker{
		Status: types.WorkerStatusPending,
		Cpu:    2000,
		Memory: 2000,
		Gpu:    "",
	}

	// Create a new worker
	err = wb.workerRepo.AddWorker(newWorker)
	assert.Nil(t, err)

	firstRequest := &types.ContainerRequest{
		Cpu:    1000,
		Memory: 1000,
		Gpu:    "",
	}

	secondRequest := &types.ContainerRequest{
		Cpu:    1000,
		Memory: 1000,
		Gpu:    "",
	}

	// Select a worker for the request
	worker, err := wb.selectWorker(firstRequest)
	assert.Nil(t, err)
	assert.Equal(t, newWorker.Gpu, worker.Gpu)

	err = wb.scheduleRequest(worker, firstRequest)
	assert.Nil(t, err)

	worker, err = wb.selectWorker(secondRequest)
	assert.Nil(t, err)
	assert.Equal(t, newWorker.Gpu, worker.Gpu)

	err = wb.scheduleRequest(worker, secondRequest)
	assert.Nil(t, err)

	updatedWorker, err := wb.workerRepo.GetWorkerById(newWorker.Id)
	assert.Nil(t, err)
	assert.Equal(t, int64(0), updatedWorker.Cpu)
	assert.Equal(t, int64(0), updatedWorker.Memory)
	assert.Equal(t, "", updatedWorker.Gpu)
	assert.Equal(t, types.WorkerStatusPending, updatedWorker.Status)
}