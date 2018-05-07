package criterion

import (
	"log"

	"github.com/stakater/Chowkidar/pkg/config"
	"k8s.io/api/core/v1"
)

//TODO: Create a criterion matcher and refactor this
func matchesCriterion(obj interface{}, criterion config.Criterion) bool {
	log.Println("Checking for resources block on Pod: `", obj.(*v1.Pod).Name+"`")
	return arePodsResourceMissing(obj.(*v1.Pod))
}

// checks if the pod containers has resources CPU and memory
func arePodsResourceMissing(pod *v1.Pod) bool {
	// Checking whether the pod has specified resources in yaml for each container
	for _, container := range pod.Spec.Containers {
		// get the Resourcelist for limits and requests which is a map
		limits := container.Resources.Limits
		requests := container.Resources.Requests
		_, hasLimitsCPU := limits["cpu"]
		_, hasLimitsMemory := limits["memory"]

		//if resources.limits does not contain CPU and Memory
		if !(hasLimitsCPU && hasLimitsMemory) {
			return true
		}
		_, hasRequestCPU := requests["cpu"]
		_, hasRequestMemory := requests["memory"]

		//if resources.Requests does not contain CPU and Memory
		if !(hasRequestCPU && hasRequestMemory) {
			return true
		}
	}

	// has Limits and Request
	log.Println("Resource block not found, performing actions")
	return false
}
