// Much of this copied from k8s.io/kubernetes/pkg/client/leaderelection/resourcelock/configmaplock.go
package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/pkg/errors"

	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	kubeErrors "k8s.io/client-go/pkg/api/errors"
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
	DefaultLeaseDuration = 5 * time.Second

	KubePeersAnnotationKey            = "kube-peers.weave.works/peers"
	LeaderElectionRecordAnnotationKey = "kube-peers.weave.works/leader"
)

func (cml *configMapAnnotations) Init() error {
	for { // Loop only if we call Create() and it's already there
		var err error
		cml.cm, err = cml.Client.ConfigMaps(cml.Namespace).Get(cml.Name)
		if err != nil {
			if !kubeErrors.IsNotFound(err) {
				return errors.Wrapf(err, "Unable to fetch ConfigMap %s/%s", cml.Namespace, cml.Name)
			}
			cml.cm, err = cml.Client.ConfigMaps(cml.Namespace).Create(&api.ConfigMap{
				ObjectMeta: api.ObjectMeta{
					Name:      cml.Name,
					Namespace: cml.Namespace,
				},
			})
			if err != nil {
				if kubeErrors.IsAlreadyExists(err) {
					continue
				}
				return errors.Wrapf(err, "Unable to create ConfigMap %s/%s", cml.Namespace, cml.Name)
			}
		}
		break
	}
	if cml.cm.Annotations == nil {
		cml.cm.Annotations = make(map[string]string)
	}
	return nil
}

func (cml *configMapAnnotations) GetPeerList() (*peerList, error) {
	var record peerList
	if cml.cm == nil {
		return nil, errors.New("endpoint not initialized, call Init first")
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
	recordBytes, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return cml.Update(KubePeersAnnotationKey, recordBytes)
}

func (cml *configMapAnnotations) Update(key string, recordBytes []byte) error {
	if cml.cm == nil {
		return errors.New("endpoint not initialized, call Init first")
	}
	var err error
	for {
		cml.cm.Annotations[key] = string(recordBytes)
		cml.cm, err = cml.Client.ConfigMaps(cml.Namespace).Update(cml.cm)
		if err != nil && kubeErrors.IsConflict(err) {
			log.Printf("Optimistic locking conflict: trying again: %s", err)
			err = cml.Init()
			continue
		}
		break
	}
	return err
}

func (cml *configMapAnnotations) GetLockRecord() (*LeaderElectionRecord, error) {
	var record LeaderElectionRecord
	if cml.cm == nil {
		return nil, errors.New("endpoint not initialized, call Init first")
	}
	if recordBytes, found := cml.cm.Annotations[LeaderElectionRecordAnnotationKey]; found {
		if err := json.Unmarshal([]byte(recordBytes), &record); err != nil {
			return nil, err
		}
	}
	return &record, nil
}

func (cml *configMapAnnotations) UpdateLockRecord(le LeaderElectionRecord) error {
	recordBytes, err := json.Marshal(le)
	if err != nil {
		return err
	}
	if cml.cm == nil {
		return errors.New("endpoint not initialized, call Init first")
	}
	// Don't loop on optimistic lock failure; just assume someone else is now leader
	cml.cm.Annotations[LeaderElectionRecordAnnotationKey] = string(recordBytes)
	cml.cm, err = cml.Client.ConfigMaps(cml.Namespace).Update(cml.cm)
	return err
}

// Update will update and existing annotation on a given resource.
func (cml *configMapAnnotations) aquireOrRenewLeader(peerName string) bool {
	now := unversioned.Now()
	leaderElectionRecord := LeaderElectionRecord{
		HolderIdentity:       peerName,
		LeaseDurationSeconds: int(DefaultLeaseDuration / time.Second),
		RenewTime:            now,
		AcquireTime:          now,
	}

	// 1. obtain or create the ElectionRecord
	oldLeaderElectionRecord, err := cml.GetLockRecord()
	if err != nil {
		log.Printf("error retrieving resource lock: %v", err)
		return false
	}
	if oldLeaderElectionRecord == nil {
		oldLeaderElectionRecord = &leaderElectionRecord
	}

	if now.Add(time.Second*time.Duration(oldLeaderElectionRecord.LeaseDurationSeconds)).After(now.Time) &&
		oldLeaderElectionRecord.HolderIdentity != peerName {
		log.Printf("lock is held by %v and has not yet expired", oldLeaderElectionRecord.HolderIdentity)
		return false
	}

	// If we previously held the lock, don't update the AcquireTime
	if oldLeaderElectionRecord.HolderIdentity == peerName {
		leaderElectionRecord.AcquireTime = oldLeaderElectionRecord.AcquireTime
	}

	// update the lock itself
	if err = cml.UpdateLockRecord(leaderElectionRecord); err != nil {
		log.Printf("Failed to update lock: %v", err)
		return false
	}
	return true
}
