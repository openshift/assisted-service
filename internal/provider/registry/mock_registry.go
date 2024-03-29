// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/openshift/assisted-service/internal/provider/registry (interfaces: Registry)

// Package registry is a generated GoMock package.
package registry

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	provider "github.com/openshift/assisted-service/internal/provider"
	models "github.com/openshift/assisted-service/models"
)

// MockRegistry is a mock of Registry interface.
type MockRegistry struct {
	ctrl     *gomock.Controller
	recorder *MockRegistryMockRecorder
}

// MockRegistryMockRecorder is the mock recorder for MockRegistry.
type MockRegistryMockRecorder struct {
	mock *MockRegistry
}

// NewMockRegistry creates a new mock instance.
func NewMockRegistry(ctrl *gomock.Controller) *MockRegistry {
	mock := &MockRegistry{ctrl: ctrl}
	mock.recorder = &MockRegistryMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockRegistry) EXPECT() *MockRegistryMockRecorder {
	return m.recorder
}

// Get mocks base method.
func (m *MockRegistry) Get(arg0 *models.Platform) (provider.Provider, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", arg0)
	ret0, _ := ret[0].(provider.Provider)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockRegistryMockRecorder) Get(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockRegistry)(nil).Get), arg0)
}

// Register mocks base method.
func (m *MockRegistry) Register(arg0 provider.Provider) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "Register", arg0)
}

// Register indicates an expected call of Register.
func (mr *MockRegistryMockRecorder) Register(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Register", reflect.TypeOf((*MockRegistry)(nil).Register), arg0)
}
