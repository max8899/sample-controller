package vm

import (
	"k8s.io/apimachinery/pkg/types"
)

type VM struct {
	ID   types.UID `json:"id"`
	Name string    `json:"name"`
}

type VMStatus struct {
	VMID           types.UID `json:"vmId"`
	CPUUtilization int32     `json:"cpuUtilization"`
}

type Interface interface {
	Create(name string) (*VM, error)
	List() ([]VM, error)
	Get(uuid types.UID) (*VM, error)
	Check(name string) error
	GetStatus(uuid types.UID) (*VMStatus, error)
	Delete(uuid types.UID) error
}

type fakeVMManager struct{}

func NewVMManager() Interface {
	return &fakeVMManager{}
}

func (*fakeVMManager) Create(string) (*VM, error) {
	//return nil, errors.NewBadRequest("Not implement")
	return &VM{}, nil
}

func (*fakeVMManager) List() ([]VM, error) {
	return nil, nil
}

func (*fakeVMManager) Get(uuid types.UID) (*VM, error) {
	//return nil, errors.NewBadRequest("Not implement")
	return &VM{}, nil
}

func (*fakeVMManager) Check(name string) error {
	return nil
}

func (*fakeVMManager) GetStatus(uuid types.UID) (*VMStatus, error) {
	return &VMStatus{}, nil
}

func (*fakeVMManager) Delete(uuid types.UID) error {
	return nil
}
