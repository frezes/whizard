/*
Copyright 2021 The KubeSphere authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/kubesphere/paodin-monitoring/pkg/api/monitoring/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// AgentLister helps list Agents.
// All objects returned here must be treated as read-only.
type AgentLister interface {
	// List lists all Agents in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.Agent, err error)
	// Agents returns an object that can list and get Agents.
	Agents(namespace string) AgentNamespaceLister
	AgentListerExpansion
}

// agentLister implements the AgentLister interface.
type agentLister struct {
	indexer cache.Indexer
}

// NewAgentLister returns a new AgentLister.
func NewAgentLister(indexer cache.Indexer) AgentLister {
	return &agentLister{indexer: indexer}
}

// List lists all Agents in the indexer.
func (s *agentLister) List(selector labels.Selector) (ret []*v1alpha1.Agent, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Agent))
	})
	return ret, err
}

// Agents returns an object that can list and get Agents.
func (s *agentLister) Agents(namespace string) AgentNamespaceLister {
	return agentNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// AgentNamespaceLister helps list and get Agents.
// All objects returned here must be treated as read-only.
type AgentNamespaceLister interface {
	// List lists all Agents in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.Agent, err error)
	// Get retrieves the Agent from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.Agent, error)
	AgentNamespaceListerExpansion
}

// agentNamespaceLister implements the AgentNamespaceLister
// interface.
type agentNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Agents in the indexer for a given namespace.
func (s agentNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.Agent, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Agent))
	})
	return ret, err
}

// Get retrieves the Agent from the indexer for a given namespace and name.
func (s agentNamespaceLister) Get(name string) (*v1alpha1.Agent, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("agent"), name)
	}
	return obj.(*v1alpha1.Agent), nil
}
