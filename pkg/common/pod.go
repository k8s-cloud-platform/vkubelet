/*
Copyright 2022 The KCP Authors.

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

package common

import (
	v1 "k8s.io/api/core/v1"
)

// PodRequestsAndLimits returns a dictionary of all defined resources summed up for all
// containers of the pod. If PodOverhead feature is enabled, pod overhead is added to the
// total container resource requests and to the total container limits which have a
// non-zero quantity.
func PodRequestsAndLimits(pod *v1.Pod) (reqs, limits v1.ResourceList) {
	return PodRequestsAndLimitsReuse(pod, nil, nil)
}

// PodRequestsAndLimitsWithoutOverhead will create a dictionary of all defined resources summed up for all
// containers of the pod.
func PodRequestsAndLimitsWithoutOverhead(pod *v1.Pod) (reqs, limits v1.ResourceList) {
	reqs = make(v1.ResourceList, 4)
	limits = make(v1.ResourceList, 4)
	podRequestsAndLimitsWithoutOverhead(pod, reqs, limits)

	return reqs, limits
}

func podRequestsAndLimitsWithoutOverhead(pod *v1.Pod, reqs, limits v1.ResourceList) {
	for _, container := range pod.Spec.Containers {
		addResourceList(reqs, container.Resources.Requests)
		addResourceList(limits, container.Resources.Limits)
	}
	// init containers define the minimum of any resource
	for _, container := range pod.Spec.InitContainers {
		maxResourceList(reqs, container.Resources.Requests)
		maxResourceList(limits, container.Resources.Limits)
	}
}

// PodRequestsAndLimitsReuse returns a dictionary of all defined resources summed up for all
// containers of the pod. If PodOverhead feature is enabled, pod overhead is added to the
// total container resource requests and to the total container limits which have a
// non-zero quantity. The caller may avoid allocations of resource lists by passing
// a requests and limits list to the function, which will be cleared before use.
func PodRequestsAndLimitsReuse(pod *v1.Pod, reuseReqs, reuseLimits v1.ResourceList) (reqs, limits v1.ResourceList) {
	// attempt to reuse the maps if passed, or allocate otherwise
	reqs, limits = reuseOrClearResourceList(reuseReqs), reuseOrClearResourceList(reuseLimits)

	podRequestsAndLimitsWithoutOverhead(pod, reqs, limits)

	// if PodOverhead feature is supported, add overhead for running a pod
	// to the sum of requests and to non-zero limits:
	if pod.Spec.Overhead != nil {
		addResourceList(reqs, pod.Spec.Overhead)

		for name, quantity := range pod.Spec.Overhead {
			if value, ok := limits[name]; ok && !value.IsZero() {
				value.Add(quantity)
				limits[name] = value
			}
		}
	}

	return
}

// reuseOrClearResourceList is a helper for avoiding excessive allocations of
// resource lists within the inner loop of resource calculations.
func reuseOrClearResourceList(reuse v1.ResourceList) v1.ResourceList {
	if reuse == nil {
		return make(v1.ResourceList, 4)
	}
	for k := range reuse {
		delete(reuse, k)
	}
	return reuse
}

// addResourceList adds the resources in newList to list.
func addResourceList(list, newList v1.ResourceList) {
	for name, quantity := range newList {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
		} else {
			value.Add(quantity)
			list[name] = value
		}
	}
}

// maxResourceList sets list to the greater of list/newList for every resource in newList
func maxResourceList(list, newList v1.ResourceList) {
	for name, quantity := range newList {
		if value, ok := list[name]; !ok || quantity.Cmp(value) > 0 {
			list[name] = quantity.DeepCopy()
		}
	}
}
