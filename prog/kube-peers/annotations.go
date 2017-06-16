// Much of this copied from k8s.io/kubernetes/pkg/client/leaderelection/resourcelock/configmaplock.go
package main

import (
	"encoding/json"
	"errors"

	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/pkg/api/unversioned"
	api "k8s.io/client-go/pkg/api/v1"
)

type LeaderElectionRecord struct {
	HolderIdentity       string           `json:"holderIdentity"`
	LeaseDurationSeconds int              `json:"leaseDurationSeconds"`
	AcquireTime          unversioned.Time `json:"acquireTime"`
	RenewTime            unversioned.Time `json:"renewTime"`
}

type configMapAnnotations struct {
	Name      string
	Namespace string
	Client    corev1client.ConfigMapsGetter
	cm        *api.ConfigMap
}

func newConfigMapAnnotations(ns string, name string, client *kubernetes.Clientset) *configMapAnnotations {
	return &configMapAnnotations{
		Namespace: ns,
		Name:      name,
		Client:    client,
	}
}

type peerList struct {
	Peers []peerInfo
}

type peerInfo struct {
	PeerName string // Weave internal unique ID
	Name     string // Kubernetes node name
}

func (pl peerList) contains(peerName string) bool {
	for _, peer := range pl.Peers {
		if peer.PeerName == peerName {
			return true
		}
	}
	return false
}

func (pl *peerList) add(peerName string, name string) {
	pl.Peers = append(pl.Peers, peerInfo{PeerName: peerName, Name: name})
}

const (
	KubePeersAnnotationKey = "kube-peers.weave.works/peers"
)

func (cml *configMapAnnotations) GetPeerList() (*peerList, error) {
	var record peerList
	var err error
	cml.cm, err = cml.Client.ConfigMaps(cml.Namespace).Get(cml.Name)
	if err != nil {
		return nil, err
	}
	if cml.cm.Annotations == nil {
		cml.cm.Annotations = make(map[string]string)
	}
	if recordBytes, found := cml.cm.Annotations[KubePeersAnnotationKey]; found {
		if err := json.Unmarshal([]byte(recordBytes), &record); err != nil {
			return nil, err
		}
	}
	return &record, nil
}

// Update will update and existing annotation on a given resource.
func (cml *configMapAnnotations) UpdatePeerList(list peerList) error {
	if cml.cm == nil {
		return errors.New("endpoint not initialized, call get or create first")
	}
	recordBytes, err := json.Marshal(list)
	if err != nil {
		return err
	}
	cml.cm.Annotations[KubePeersAnnotationKey] = string(recordBytes)
	cml.cm, err = cml.Client.ConfigMaps(cml.Namespace).Update(cml.cm)
	return err
}
