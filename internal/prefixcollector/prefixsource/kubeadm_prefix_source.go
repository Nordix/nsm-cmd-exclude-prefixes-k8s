// Copyright (c) 2020-2021 Doc.ai and/or its affiliates.
//
// Copyright (c) 2022-2024 Cisco and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prefixsource

import (
	"cmd-exclude-prefixes-k8s/internal/prefixcollector"
	"cmd-exclude-prefixes-k8s/internal/utils"
	"context"
	"strings"

	apiV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/watch"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta2"

	"github.com/networkservicemesh/sdk/pkg/tools/log"
)

const (
	// KubeNamespace is KubeAdm ConfigMap namespace
	KubeNamespace = "kube-system"
	// KubeName is KubeAdm ConfigMap name
	KubeName   = "kubeadm-config"
	bufferSize = 4096
)

// KubeAdmPrefixSource is KubeAdm ConfigMap excluded prefix source
type KubeAdmPrefixSource struct {
	configMapInterface v1.ConfigMapInterface
	prefixes           *utils.SynchronizedPrefixesContainer
	ctx                context.Context
	notify             chan<- struct{}
}

// Prefixes returns prefixes from source
func (kaps *KubeAdmPrefixSource) Prefixes() []string {
	return kaps.prefixes.Load()
}

// NewKubeAdmPrefixSource creates KubeAdmPrefixSource
func NewKubeAdmPrefixSource(ctx context.Context, notify chan<- struct{}) *KubeAdmPrefixSource {
	clientSet := prefixcollector.KubernetesInterface(ctx)
	configMapInterface := clientSet.CoreV1().ConfigMaps(KubeNamespace)
	kaps := KubeAdmPrefixSource{
		configMapInterface: configMapInterface,
		ctx:                ctx,
		notify:             notify,
		prefixes:           utils.NewSynchronizedPrefixesContainer(),
	}

	go func() {
		for kaps.ctx.Err() == nil {
			kaps.watchKubeAdmConfigMap()
		}
	}()
	return &kaps
}

func (kaps *KubeAdmPrefixSource) watchKubeAdmConfigMap() {
	log.FromContext(kaps.ctx).Infof("Watch kubeadm configMap")

	configMapWatch, err := kaps.configMapInterface.Watch(kaps.ctx, metav1.ListOptions{})
	if err != nil {
		log.FromContext(kaps.ctx).Errorf("Error creating config map watch: %v", err)
		return
	}
	defer configMapWatch.Stop()

	// we should check current state after we create the watcher,
	// or else we could miss an update
	kaps.checkCurrentConfigMap()

	for {
		select {
		case <-kaps.ctx.Done():
			log.FromContext(kaps.ctx).Warn("kubeadm configMap context is canceled")
			return
		case event, ok := <-configMapWatch.ResultChan():
			if !ok {
				log.FromContext(kaps.ctx).Warn("kubeadm configMap watcher is closed")
				return
			}

			log.FromContext(kaps.ctx).Tracef("kubeadm configMap event received: %v", event)

			if event.Type == watch.Error {
				continue
			}

			configMap, ok := event.Object.(*apiV1.ConfigMap)
			if !ok || configMap.Name != KubeName {
				continue
			}

			if event.Type == watch.Deleted {
				kaps.prefixes.Store([]string(nil))
				kaps.notify <- struct{}{}
				log.FromContext(kaps.ctx).Info("kubeadm configMap deleted")
				continue
			}

			if err = kaps.setPrefixesFromConfigMap(configMap); err != nil {
				log.FromContext(kaps.ctx).Error(err)
			}
		}
	}
}

func (kaps *KubeAdmPrefixSource) checkCurrentConfigMap() {
	configMap, err := kaps.configMapInterface.Get(kaps.ctx, KubeName, metav1.GetOptions{})

	if err != nil {
		log.FromContext(kaps.ctx).Errorf("Error getting KubeAdm config map : %v", err)
		return
	}

	if err = kaps.setPrefixesFromConfigMap(configMap); err != nil {
		log.FromContext(kaps.ctx).Error("Error setting prefixes from KubeAdm config map")
	}
}

// splitPrefix splits single prefix string into list of prefixes treating the input as comma separated.
// When cluster supports both IPv4 and IPv6 we can receive combined addresses e.g. "10.244.0.0/16,fd00:10:244::/56"
func splitPrefix(prefix string) []string {
	raws := strings.Split(prefix, ",")
	var parts []string
	for _, raw := range raws {
		part := strings.TrimSpace(raw)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func (kaps *KubeAdmPrefixSource) setPrefixesFromConfigMap(configMap *apiV1.ConfigMap) error {
	clusterConfiguration := &v1beta2.ClusterConfiguration{}
	err := yaml.NewYAMLOrJSONDecoder(
		strings.NewReader(configMap.Data["ClusterConfiguration"]), bufferSize,
	).Decode(clusterConfiguration)

	if err != nil {
		log.FromContext(kaps.ctx).Errorf("error decoding cluster config: %v", err.Error())
		return err
	}

	podSubnet := clusterConfiguration.Networking.PodSubnet
	serviceSubnet := clusterConfiguration.Networking.ServiceSubnet

	if podSubnet == "" {
		log.FromContext(kaps.ctx).Error("ClusterConfiguration.Networking.PodSubnet is empty")
	}
	if serviceSubnet == "" {
		log.FromContext(kaps.ctx).Error("ClusterConfiguration.Networking.ServiceSubnet is empty")
	}

	prefixes := append(splitPrefix(podSubnet), splitPrefix(serviceSubnet)...)

	kaps.prefixes.Store(prefixes)
	kaps.notify <- struct{}{}
	log.FromContext(kaps.ctx).Infof("Prefixes sent from kubeadm source: %v", prefixes)

	return nil
}
