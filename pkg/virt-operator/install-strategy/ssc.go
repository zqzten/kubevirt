package installstrategy

import (
	"encoding/json"
	"fmt"

	secv1 "github.com/openshift/api/security/v1"
	"k8s.io/apimachinery/pkg/types"

	"kubevirt.io/client-go/log"
	"kubevirt.io/kubevirt/pkg/virt-operator/creation/rbac"
)

func (r *Reconciler) createOrUpdateSCC() error {
	sec := r.clientset.SecClient()

	if !r.stores.IsOnOpenshift {
		return nil
	}

	version, imageRegistry, id := getTargetVersionRegistryID(r.kv)

	for _, scc := range r.targetStrategy.sccs {
		var cachedSCC *secv1.SecurityContextConstraints
		scc := scc.DeepCopy()
		obj, exists, _ := r.stores.SCCCache.GetByKey(scc.Name)
		if exists {
			cachedSCC = obj.(*secv1.SecurityContextConstraints)
		}

		injectOperatorMetadata(r.kv, &scc.ObjectMeta, version, imageRegistry, id, true)
		if !exists {
			r.expectations.SCC.RaiseExpectations(r.kvKey, 1, 0)
			_, err := sec.SecurityContextConstraints().Create(scc)
			if err != nil {
				r.expectations.SCC.LowerExpectations(r.kvKey, 1, 0)
				return fmt.Errorf("unable to create SCC %+v: %v", scc, err)
			}

			log.Log.V(2).Infof("SCC %v created", scc.Name)
		} else if !objectMatchesVersion(&cachedSCC.ObjectMeta, version, imageRegistry, id, r.kv.GetGeneration()) {
			scc.ObjectMeta = *cachedSCC.ObjectMeta.DeepCopy()
			injectOperatorMetadata(r.kv, &scc.ObjectMeta, version, imageRegistry, id, true)
			_, err := sec.SecurityContextConstraints().Update(scc)
			if err != nil {
				return fmt.Errorf("Unable to update %s SecurityContextConstraints", scc.Name)
			}

			log.Log.V(2).Infof("SecurityContextConstraints %s updated", scc.Name)
		} else {
			log.Log.V(4).Infof("SCC %s is up to date", scc.Name)
		}

	}

	return nil
}

func (r *Reconciler) removeKvServiceAccountsFromDefaultSCC(targetNamespace string) error {
	var remainedUsersList []string

	SCCObj, exists, err := r.stores.SCCCache.GetByKey("privileged")
	if err != nil {
		return err
	} else if !exists {
		return nil
	}

	SCC, ok := SCCObj.(*secv1.SecurityContextConstraints)
	if !ok {
		return fmt.Errorf("couldn't cast object to SecurityContextConstraints: %+v", SCCObj)
	}

	modified := false
	kvServiceAccounts := rbac.GetKubevirtComponentsServiceAccounts(targetNamespace)
	for _, acc := range SCC.Users {
		if _, ok := kvServiceAccounts[acc]; !ok {
			remainedUsersList = append(remainedUsersList, acc)
		} else {
			modified = true
		}
	}

	if modified {
		oldUserBytes, err := json.Marshal(SCC.Users)
		if err != nil {
			return err
		}
		userBytes, err := json.Marshal(remainedUsersList)
		if err != nil {
			return err
		}

		test := fmt.Sprintf(`{ "op": "test", "path": "/users", "value": %s }`, string(oldUserBytes))
		patch := fmt.Sprintf(`{ "op": "replace", "path": "/users", "value": %s }`, string(userBytes))

		_, err = r.clientset.SecClient().SecurityContextConstraints().Patch("privileged", types.JSONPatchType, []byte(fmt.Sprintf("[ %s, %s ]", test, patch)))
		if err != nil {
			return fmt.Errorf("unable to patch scc: %v", err)
		}
	}

	return nil
}
