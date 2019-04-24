package vm

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type VM struct {
	ID           types.UID `json:"id"`
	Name           string `json:"name"`
}

type VMStatus struct {
	VMID           types.UID `json:"vmId"`
	CPUUtilization int32     `json:"cpuUtilization"`
}

type Interface interface {
	Create(name string)(*VM, *errors.StatusError)
	List()([]VM, *errors.StatusError)
	Get(uuid types.UID)(*VM, *errors.StatusError)
	Check(name string)(*errors.StatusError)
	GetStatus(uuid types.UID)(*VMStatus, *errors.StatusError)
	Delete(uuid types.UID)*errors.StatusError
}

type fakeVMManager struct {}

func NewVMManager() Interface {
	return &fakeVMManager{}
}

func (*fakeVMManager) Create(string) (*VM, *errors.StatusError){
	return &VM{}, nil
}

func (*fakeVMManager) List()([]VM, *errors.StatusError){
	return nil, nil
}

func (*fakeVMManager) Get(uuid types.UID)(*VM, *errors.StatusError) {
	return &VM{}, nil
}

func (*fakeVMManager) Check(name string)( *errors.StatusError) {
	return nil
}

func (*fakeVMManager) GetStatus(uuid types.UID)(*VMStatus, *errors.StatusError) {
	return &VMStatus{}, nil
}

func (*fakeVMManager) Delete(uuid types.UID)*errors.StatusError{
	return nil
}


