/*
Copyright 2021.

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

package controllers

import (
	"context"
	RegistryApi "github.com/dweber019/apicurio-registry-artifact-operator/registry_api"
	"k8s.io/apimachinery/pkg/api/errors"
	"strings"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	artifactv1alpha1 "github.com/dweber019/apicurio-registry-artifact-operator/api/v1alpha1"
)

// ApicurioReconciler reconciles a Apicurio object
type ApicurioReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=artifact.w3tec.ch,resources=apicurios,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=artifact.w3tec.ch,resources=apicurios/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=artifact.w3tec.ch,resources=apicurios/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Apicurio object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *ApicurioReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("apicurio", req.NamespacedName)

	apicurioArtifact := &artifactv1alpha1.Apicurio{}
	err := r.Get(ctx, req.NamespacedName, apicurioArtifact)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Artifact resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get artifact")
		return ctrl.Result{}, err
	}

	registryApiClient, err := RegistryApi.NewClientWithResponses(apicurioArtifact.Spec.RegistryApiEndpoint)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update artifact with POST /artifacts

	var ifExists = "RETURN_OR_UPDATE"
	var artifactType = string(apicurioArtifact.Spec.Type)
	response, err := registryApiClient.CreateArtifactWithBodyWithResponse(ctx, &RegistryApi.CreateArtifactParams{
		IfExists:              &ifExists,
		XRegistryArtifactType: &artifactType,
		XRegistryArtifactId:   &apicurioArtifact.Spec.Id,
	}, apicurioArtifact.Spec.ContentType, strings.NewReader(apicurioArtifact.Spec.Content))
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update metadata
	_, err = registryApiClient.UpdateArtifactVersionMetaDataWithResponse(ctx, response.JSON200.Id, int(response.JSON200.Version), RegistryApi.UpdateArtifactVersionMetaDataJSONRequestBody{
		Description: &apicurioArtifact.Spec.Description,
		Labels:      &apicurioArtifact.Spec.Labels,
		Name:        &apicurioArtifact.Spec.Name,
		Properties: &RegistryApi.Properties{
			AdditionalProperties: apicurioArtifact.Spec.Properties,
		},
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update state if configured
	if &apicurioArtifact.Spec.State != nil {
		registryApiClient.UpdateArtifactVersionState(ctx, response.JSON200.Id, int(response.JSON200.Version), RegistryApi.UpdateArtifactVersionStateJSONRequestBody{
			State: RegistryApi.ArtifactState(apicurioArtifact.Spec.State),
		})
	}

	// Get artifact rules current configrtion
	if &apicurioArtifact.Spec.RuleValidity != nil || &apicurioArtifact.Spec.RuleCompatibility != nil {
		artifactRules, err := registryApiClient.ListArtifactRulesWithResponse(ctx, response.JSON200.Id)
		if err != nil {
			return ctrl.Result{}, err
		}

		// Update rule VALIDITY if configured
		if &apicurioArtifact.Spec.RuleValidity != nil {
			var ruleType = RegistryApi.RuleType_VALIDITY
			if ContainsRuleType(*artifactRules.JSON200, ruleType) {
				registryApiClient.UpdateArtifactRuleConfig(ctx, response.JSON200.Id, string(ruleType), RegistryApi.UpdateArtifactRuleConfigJSONRequestBody{
					Config: string(apicurioArtifact.Spec.RuleValidity),
					Type:   &ruleType,
				})
			} else {
				registryApiClient.CreateArtifactRule(ctx, response.JSON200.Id, RegistryApi.CreateArtifactRuleJSONRequestBody{
					Config: string(apicurioArtifact.Spec.RuleValidity),
					Type:   &ruleType,
				})
			}

		}

		// Update rule VALIDITY if configured
		if &apicurioArtifact.Spec.RuleCompatibility != nil {
			var ruleType = RegistryApi.RuleType_COMPATIBILITY
			if ContainsRuleType(*artifactRules.JSON200, ruleType) {
				registryApiClient.UpdateArtifactRuleConfig(ctx, response.JSON200.Id, string(ruleType), RegistryApi.UpdateArtifactRuleConfigJSONRequestBody{
					Config: string(apicurioArtifact.Spec.RuleCompatibility),
					Type:   &ruleType,
				})
			} else {
				registryApiClient.CreateArtifactRule(ctx, response.JSON200.Id, RegistryApi.CreateArtifactRuleJSONRequestBody{
					Config: string(apicurioArtifact.Spec.RuleCompatibility),
					Type:   &ruleType,
				})
			}
		}

	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApicurioReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&artifactv1alpha1.Apicurio{}).
		Complete(r)
}

func ContainsRuleType(a []RegistryApi.RuleType, x RegistryApi.RuleType) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}
