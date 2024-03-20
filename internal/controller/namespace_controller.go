package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	akuityiov1 "github.com/anubhav06/nsc-controller/api/v1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
)

type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=akuity.io.my.domain,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=akuity.io.my.domain,resources=namespaces/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=akuity.io.my.domain,resources=namespaces/finalizers,verbs=update

func (r *NamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := log.FromContext(ctx)

	namespace := &corev1.Namespace{}
	if err := r.fetchNamespace(ctx, req.NamespacedName, namespace, log); err != nil {
		return ctrl.Result{}, err
	}

	classLabel, ok := namespace.Labels["namespaceclass.akuity.io/name"]
	if !ok {
		log.Info("Namespace does not have a class label", "namespace", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	namespaceClass := &akuityiov1.NamespaceClass{}
	if err := r.fetchNamespaceClass(ctx, classLabel, namespaceClass, log); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.handleResources(ctx, namespace, namespaceClass, log); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.handleNamespaceAnnotations(ctx, namespace, namespaceClass, log); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// Get the namespace
func (r *NamespaceReconciler) fetchNamespace(ctx context.Context, namespacedName types.NamespacedName, namespace *corev1.Namespace, log logr.Logger) error {
	if err := r.Get(ctx, namespacedName, namespace); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Namespace not found", "namespace", namespacedName)
			return nil
		}
		log.Error(err, "Failed to get namespace", "namespace", namespacedName)
		return err
	}
	return nil
}

// Get the NamespaceClass
func (r *NamespaceReconciler) fetchNamespaceClass(ctx context.Context, classLabel string, namespaceClass *akuityiov1.NamespaceClass, log logr.Logger) error {
	if err := r.Get(ctx, client.ObjectKey{Name: classLabel}, namespaceClass); err != nil {
		if errors.IsNotFound(err) {
			log.Info("NamespaceClass not found", "namespaceclass", classLabel)
			return nil
		}
		log.Error(err, "Failed to get namespace class", "namespaceclass", classLabel)
		return err
	}
	return nil
}

// Handle the creation and updating of resources defined in the NamespaceClass
func (r *NamespaceReconciler) handleResources(ctx context.Context, namespace *corev1.Namespace, namespaceClass *akuityiov1.NamespaceClass, log logr.Logger) error {
	for _, resource := range namespaceClass.Spec.Resources {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(resource.Raw); err != nil {
			log.Error(err, "Failed to unmarshal resource", "resource", resource)
			return err
		}

		obj.SetNamespace(namespace.Name)
		key := client.ObjectKeyFromObject(obj)
		if err := r.Get(ctx, key, obj); err != nil {
			if errors.IsNotFound(err) {
				if err := r.Create(ctx, obj); err != nil {
					log.Error(err, "Failed to create resource", "resource", obj)
					return err
				}
				log.Info("Created resource", "resource", obj)

			} else {
				log.Error(err, "Failed to get resource", "resource", obj)
				return err
			}
		} else {
			if err := r.Update(ctx, obj); err != nil {
				log.Error(err, "Failed to update resource", "resource", obj)
				return err
			}
			log.Info("Updated resource", "resource", obj)
		}

		// Add the resource in namespaceClass status, if it is not already present
		if !r.resourceExistsInNamespaceClassStatus(obj, namespaceClass) {
			namespaceClass.Status.Resources = append(namespaceClass.Status.Resources, akuityiov1.ResourceStatus{
				Name:       obj.GetName(),
				APIVersion: obj.GetAPIVersion(),
				Kind:       obj.GetKind(),
			})
			if err := r.Status().Update(ctx, namespaceClass); err != nil {
				log.Error(err, "Failed to update namespace class status", "namespaceclass", namespaceClass)
				return err
			}
		}
	}
	return nil
}

// Handles the deletion of resources that are no longer defined in the NamespaceClass.
// It uses the last-name annotation to check if the NamespaceClass has changed
func (r *NamespaceReconciler) handleNamespaceAnnotations(ctx context.Context, namespace *corev1.Namespace, namespaceClass *akuityiov1.NamespaceClass, log logr.Logger) error {
	if namespace.Annotations == nil {
		namespace.Annotations = make(map[string]string)
	}

	if lastName, ok := namespace.Annotations["namespaceclass.akuity.io/last-name"]; !ok {
		namespace.Annotations["namespaceclass.akuity.io/last-name"] = namespaceClass.Name
	} else if lastName != namespaceClass.Name {
		log.Info("Namespace class has changed from " + lastName + " to " + namespaceClass.Name)
		namespace.Annotations["namespaceclass.akuity.io/last-name"] = namespaceClass.Name
		if err := r.handleNamespaceClassChange(ctx, namespace, lastName, namespaceClass, log); err != nil {
			return err
		}
	}
	if err := r.Update(ctx, namespace); err != nil {
		log.Error(err, "Failed to update namespace", "namespace", namespace)
		return err
	}

	if err := r.handleResourcesDeletion(ctx, namespace, namespaceClass, log); err != nil {
		return err
	}

	return nil
}

// Delete resources that are no longer defined in the new NamespaceClass
func (r *NamespaceReconciler) handleNamespaceClassChange(ctx context.Context, namespace *corev1.Namespace, lastName string, namespaceClass *akuityiov1.NamespaceClass, log logr.Logger) error {
	oldNamespaceClass := &akuityiov1.NamespaceClass{}
	if err := r.fetchNamespaceClass(ctx, lastName, oldNamespaceClass, log); err != nil {
		return err
	}

	for _, resource := range oldNamespaceClass.Spec.Resources {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(resource.Raw); err != nil {
			log.Error(err, "Failed to unmarshal resource", "resource", resource)
			return err
		}

		obj.SetNamespace(namespace.Name)
		key := client.ObjectKeyFromObject(obj)
		if err := r.Get(ctx, key, obj); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			log.Error(err, "Failed to get resource", "resource", obj)
			return err
		}

		if existsInNewNamespaceClass := r.resourceExistsInNamespaceClass(obj, namespaceClass); !existsInNewNamespaceClass {
			if err := r.Delete(ctx, obj); err != nil {
				log.Error(err, "Failed to delete resource", "resource", obj)
				return err
			}
			log.Info("Deleted resource", "resource", obj)
		}
	}
	return nil
}

// Handle the deletion of resources that are no longer defined in the existing NamespaceClass
func (r *NamespaceReconciler) handleResourcesDeletion(ctx context.Context, namespace *corev1.Namespace, namespaceClass *akuityiov1.NamespaceClass, log logr.Logger) error {
	for _, resource := range namespaceClass.Status.Resources {
		obj := &unstructured.Unstructured{}
		obj.SetNamespace(namespace.Name)
		obj.SetAPIVersion(resource.APIVersion)
		obj.SetKind(resource.Kind)
		obj.SetName(resource.Name)

		// Check if there is any extra status.resource that is not present in the namespaceClass.spec.resources
		if !r.resourceExistsInNamespaceClass(obj, namespaceClass) {
			if err := r.Delete(ctx, obj); err != nil {
				log.Error(err, "Failed to delete resource", "resource", obj)
				return err
			}
			log.Info("Deleted resource", "resource", obj)

			// Remove the resource from namespaceClass status
			for i, statusResource := range namespaceClass.Status.Resources {
				if statusResource.Name == obj.GetName() && statusResource.APIVersion == obj.GetAPIVersion() && statusResource.Kind == obj.GetKind() {
					namespaceClass.Status.Resources = append(namespaceClass.Status.Resources[:i], namespaceClass.Status.Resources[i+1:]...)
					if err := r.Status().Update(ctx, namespaceClass); err != nil {
						log.Error(err, "Failed to update namespace class status", "namespaceclass", namespaceClass)
						return err
					}
				}
			}
		}
	}
	return nil
}

// Check if the resource exists in the NamespaceClass status
func (r *NamespaceReconciler) resourceExistsInNamespaceClassStatus(obj *unstructured.Unstructured, namespaceClass *akuityiov1.NamespaceClass) bool {
	for _, resource := range namespaceClass.Status.Resources {
		if resource.Name == obj.GetName() && resource.APIVersion == obj.GetAPIVersion() && resource.Kind == obj.GetKind() {
			return true
		}
	}
	return false
}

// Check if the same resource exists in the new NamespaceClass
func (r *NamespaceReconciler) resourceExistsInNamespaceClass(obj *unstructured.Unstructured, namespaceClass *akuityiov1.NamespaceClass) bool {
	for _, newResource := range namespaceClass.Spec.Resources {
		newObj := &unstructured.Unstructured{}
		if err := newObj.UnmarshalJSON(newResource.Raw); err != nil {
			return false
		}
		if newObj.GroupVersionKind() == obj.GroupVersionKind() {
			return true
		}
	}
	return false
}

func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{}).
		Watches(&akuityiov1.NamespaceClass{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				return r.mapNamespaceClassToNamespaces(o)
			}),
		).
		Complete(r)
}

// mapNamespaceClassToNamespaces maps a NamespaceClass to a list of Namespaces that use it.
func (r *NamespaceReconciler) mapNamespaceClassToNamespaces(obj client.Object) []reconcile.Request {
	var reqs []reconcile.Request

	// Get the NamespaceClass from the object
	namespaceClass, ok := obj.(*akuityiov1.NamespaceClass)
	if !ok {
		// If the object is not a NamespaceClass, ignore it
		return nil
	}

	// List all Namespace resources
	var namespaces corev1.NamespaceList
	if err := r.List(context.Background(), &namespaces); err != nil {
		// handle error
		return nil
	}

	// Iterate over the namespaces and enqueue a request for each namespace that uses the NamespaceClass
	for _, ns := range namespaces.Items {
		if ns.Labels["namespaceclass.akuity.io/name"] == namespaceClass.Name {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: ns.Name,
				},
			})
		}
	}

	return reqs
}
