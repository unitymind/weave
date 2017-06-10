package main

import (
	"encoding/json"
	"errors"

	"k8s.io/client-go/kubernetes"
	betaClient "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	"k8s.io/client-go/pkg/api/unversioned"
	beta "k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

type LeaderElectionRecord struct {
	HolderIdentity       string           `json:"holderIdentity"`
	LeaseDurationSeconds int              `json:"leaseDurationSeconds"`
	AcquireTime          unversioned.Time `json:"acquireTime"`
	RenewTime            unversioned.Time `json:"renewTime"`
}

type daemonSetAnnotations struct {
	Name      string
	Namespace string
	Client    betaClient.DaemonSetsGetter
	ds        *beta.DaemonSet
}

func newDaemonSetAnnotations(ns string, name string, client *kubernetes.Clientset) *daemonSetAnnotations {
	return &daemonSetAnnotations{
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

func (dsa *daemonSetAnnotations) GetPeerList() (*peerList, error) {
	var record peerList
	var err error
	dsa.ds, err = dsa.Client.DaemonSets(dsa.Namespace).Get(dsa.Name)
	if err != nil {
		return nil, err
	}
	if dsa.ds.Annotations == nil {
		dsa.ds.Annotations = make(map[string]string)
	}
	if recordBytes, found := dsa.ds.Annotations[KubePeersAnnotationKey]; found {
		if err := json.Unmarshal([]byte(recordBytes), &record); err != nil {
			return nil, err
		}
	}
	return &record, nil
}

// Update will update and existing annotation on a given resource.
func (dsa *daemonSetAnnotations) UpdatePeerList(list peerList) error {
	if dsa.ds == nil {
		return errors.New("endpoint not initialized, call get or create first")
	}
	recordBytes, err := json.Marshal(list)
	if err != nil {
		return err
	}
	dsa.ds.Annotations[KubePeersAnnotationKey] = string(recordBytes)
	dsa.ds, err = dsa.Client.DaemonSets(dsa.Namespace).Update(dsa.ds)
	return err
}
