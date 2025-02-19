package reconcilers_test

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	api "sigs.k8s.io/hierarchical-namespaces/api/v1alpha2"
	"sigs.k8s.io/hierarchical-namespaces/internal/config"
)

var _ = Describe("Hierarchy", func() {
	ctx := context.Background()

	var (
		fooName string
		barName string
	)

	BeforeEach(func() {
		fooName = createNS(ctx, "foo")
		barName = createNS(ctx, "bar")
		config.ExcludedNamespaces = nil
	})

	It("should set a child on the parent", func() {
		fooHier := newHierarchy(fooName)
		fooHier.Spec.Parent = barName
		updateHierarchy(ctx, fooHier)
		Eventually(hasChild(ctx, barName, fooName)).Should(Equal(true))
	})

	It("should remove the hierarchyconfiguration singleton in an excluded namespacee", func() {
		// Set the excluded-namespace "kube-system"'s parent to "bar".
		config.ExcludedNamespaces = map[string]bool{"kube-system": true}
		exHier := newHierarchy("kube-system")
		exHier.Spec.Parent = barName
		updateHierarchy(ctx, exHier)

		// Verify the hierarchyconfiguration singleton is deleted.
		Eventually(canGetHierarchy(ctx, "kube-system")).Should(Equal(false))
	})

	It("should set IllegalParent condition if the parent is an excluded namespace", func() {
		// Set bar's parent to the excluded-namespace "kube-system".
		config.ExcludedNamespaces = map[string]bool{"kube-system": true}
		barHier := newHierarchy(barName)
		barHier.Spec.Parent = "kube-system"
		updateHierarchy(ctx, barHier)
		// Bar singleton should have "IllegalParent" and no "ParentMissing" condition.
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonIllegalParent)).Should(Equal(true))
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonParentMissing)).Should(Equal(false))
	})

	It("should set ParentMissing condition if the parent is missing", func() {
		// Set up the parent-child relationship
		barHier := newHierarchy(barName)
		barHier.Spec.Parent = "brumpf"
		updateHierarchy(ctx, barHier)
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonParentMissing)).Should(Equal(true))
	})

	It("should unset ParentMissing condition if the parent is later created", func() {
		// Set up the parent-child relationship with the missing name
		brumpfName := createNSName("brumpf")
		barHier := newHierarchy(barName)
		barHier.Spec.Parent = brumpfName
		updateHierarchy(ctx, barHier)
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonParentMissing)).Should(Equal(true))

		// Create the missing parent
		brumpfNS := &corev1.Namespace{}
		brumpfNS.Name = brumpfName
		Expect(k8sClient.Create(ctx, brumpfNS)).Should(Succeed())

		// Ensure the condition is resolved on the child
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonParentMissing)).Should(Equal(false))

		// Ensure the child is listed on the parent
		Eventually(hasChild(ctx, brumpfName, barName)).Should(Equal(true))
	})

	It("should set AncestorHaltActivities condition if any ancestor has critical condition", func() {
		// Set up the parent-child relationship
		barHier := newHierarchy(barName)
		barHier.Spec.Parent = "brumpf"
		updateHierarchy(ctx, barHier)
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonParentMissing)).Should(Equal(true))

		// Set bar as foo's parent
		fooHier := newHierarchy(fooName)
		fooHier.Spec.Parent = barName
		updateHierarchy(ctx, fooHier)
		Eventually(hasCondition(ctx, fooName, api.ConditionActivitiesHalted, api.ReasonAncestor)).Should(Equal(true))
	})

	It("should unset AncestorHaltActivities condition if critical conditions in ancestors are gone", func() {
		// Set up the parent-child relationship with the missing name
		brumpfName := createNSName("brumpf")
		barHier := newHierarchy(barName)
		barHier.Spec.Parent = brumpfName
		updateHierarchy(ctx, barHier)
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonParentMissing)).Should(Equal(true))

		// Set bar as foo's parent
		fooHier := newHierarchy(fooName)
		fooHier.Spec.Parent = barName
		updateHierarchy(ctx, fooHier)
		Eventually(hasCondition(ctx, fooName, api.ConditionActivitiesHalted, api.ReasonAncestor)).Should(Equal(true))

		// Create the missing parent
		brumpfNS := &corev1.Namespace{}
		brumpfNS.Name = brumpfName
		Expect(k8sClient.Create(ctx, brumpfNS)).Should(Succeed())

		// Ensure the condition is resolved on the child
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonParentMissing)).Should(Equal(false))

		// Ensure the child is listed on the parent
		Eventually(hasChild(ctx, brumpfName, barName)).Should(Equal(true))

		// Ensure foo is enqueued and thus get AncestorHaltActivities condition updated after
		// critical conditions are resolved in bar.
		Eventually(hasCondition(ctx, fooName, api.ConditionActivitiesHalted, api.ReasonAncestor)).Should(Equal(false))
	})

	It("should set InCycle condition if a self-cycle is detected", func() {
		fooHier := newHierarchy(fooName)
		fooHier.Spec.Parent = fooName
		updateHierarchy(ctx, fooHier)
		Eventually(hasCondition(ctx, fooName, api.ConditionActivitiesHalted, api.ReasonInCycle)).Should(Equal(true))
	})

	It("should set InCycle condition if a cycle is detected", func() {
		// Set up initial hierarchy
		setParent(ctx, barName, fooName)
		Eventually(hasChild(ctx, fooName, barName)).Should(Equal(true))

		// Break it
		setParent(ctx, fooName, barName)
		Eventually(hasCondition(ctx, fooName, api.ConditionActivitiesHalted, api.ReasonInCycle)).Should(Equal(true))
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonInCycle)).Should(Equal(true))

		// Fix it
		setParent(ctx, fooName, "")
		Eventually(hasCondition(ctx, fooName, api.ConditionActivitiesHalted, api.ReasonInCycle)).Should(Equal(false))
		Eventually(hasCondition(ctx, barName, api.ConditionActivitiesHalted, api.ReasonInCycle)).Should(Equal(false))
	})

	It("should have a tree label", func() {
		// Make bar a child of foo
		setParent(ctx, barName, fooName)

		// Verify all the labels
		Eventually(getLabel(ctx, barName, fooName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, barName, barName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, fooName, fooName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
	})

	It("should propagate external tree labels", func() {
		// Create an external namespace baz
		l := map[string]string{
			"ext2" + api.LabelTreeDepthSuffix: "2",
			"ext1" + api.LabelTreeDepthSuffix: "1",
		}
		a := map[string]string{api.AnnotationManagedBy: "others"}
		bazName := createNSWithLabelAnnotation(ctx, "baz", l, a)

		// Make bar a child of baz
		setParent(ctx, barName, bazName)

		// Verify all the labels
		Eventually(getLabel(ctx, barName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal("3"))
		Eventually(getLabel(ctx, barName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		// Even if baz doesn't have the tree label of itself with depth 0 when created,
		// the tree label of itself is added automatically.
		Eventually(getLabel(ctx, barName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, barName, barName+api.LabelTreeDepthSuffix)).Should(Equal("0"))

		Eventually(getLabel(ctx, bazName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, bazName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
	})

	It("should propagate external tree labels if the internal namespace is converted to external", func() {
		// Make bar a child of foo
		setParent(ctx, barName, fooName)

		// Verify all the labels
		Eventually(getLabel(ctx, barName, fooName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, barName, barName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, fooName, fooName+api.LabelTreeDepthSuffix)).Should(Equal("0"))

		// Convert foo from an internal namespace to an external namespace by adding
		// the "managed-by: others" annotation.
		ns := getNamespace(ctx, fooName)
		ns.SetAnnotations(map[string]string{api.AnnotationManagedBy: "others"})
		l := map[string]string{
			"ext2" + api.LabelTreeDepthSuffix: "2",
			"ext1" + api.LabelTreeDepthSuffix: "1",
		}
		ns.SetLabels(l)
		updateNamespace(ctx, ns)

		// Verify all the labels
		Eventually(getLabel(ctx, barName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal("3"))
		Eventually(getLabel(ctx, barName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		// Even if foo doesn't have the tree label of itself with depth 0 when created,
		// the tree label of itself is added automatically.
		Eventually(getLabel(ctx, barName, fooName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, barName, barName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, fooName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, fooName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, fooName, fooName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
	})

	It("should remove external tree labels if the external namespace is converted to internal", func() {
		// Create an external namespace baz
		l := map[string]string{
			"ext2" + api.LabelTreeDepthSuffix: "2",
			"ext1" + api.LabelTreeDepthSuffix: "1",
		}
		a := map[string]string{api.AnnotationManagedBy: "others"}
		bazName := createNSWithLabelAnnotation(ctx, "baz", l, a)

		// Make bar a child of baz
		setParent(ctx, barName, bazName)

		// Verify all the labels
		Eventually(getLabel(ctx, barName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal("3"))
		Eventually(getLabel(ctx, barName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		// Even if baz doesn't have the tree label of itself with depth 0 when created,
		// the tree label of itself is added automatically.
		Eventually(getLabel(ctx, barName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, barName, barName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, bazName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))

		// Convert baz from an external namespace to an internal namespace by removing
		// the "managed-by: others" annotation.
		ns := getNamespace(ctx, bazName)
		ns.SetAnnotations(map[string]string{})
		updateNamespace(ctx, ns)

		// Verify all the labels
		Eventually(getLabel(ctx, barName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, barName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, barName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, barName, barName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, "ext2"+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, bazName, "ext1"+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
	})

	It("should update labels when parent is changed", func() {
		// Set up key-value pair for non-HNC label
		const keyName = "key"
		const valueName = "value"

		// Set up initial hierarchy
		bazName := createNSWithLabel(ctx, "baz", map[string]string{keyName: valueName})
		bazHier := newHierarchy(bazName)
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, keyName)).Should(Equal(valueName))

		// Make baz as a child of bar
		bazHier.Spec.Parent = barName
		updateHierarchy(ctx, bazHier)

		// Verify all labels on baz after set bar as parent
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, barName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, bazName, keyName)).Should(Equal(valueName))

		// Change parent to foo
		bazHier.Spec.Parent = fooName
		updateHierarchy(ctx, bazHier)

		// Verify all labels on baz after change parent to foo
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, fooName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, bazName, barName+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, bazName, keyName)).Should(Equal(valueName))
	})

	It("should update labels when parent is removed", func() {
		// Set up key-value pair for non-HNC label
		const keyName = "key"
		const valueName = "value"

		// Set up initial hierarchy
		bazName := createNSWithLabel(ctx, "baz", map[string]string{keyName: valueName})
		bazHier := newHierarchy(bazName)
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, keyName)).Should(Equal(valueName))

		// Make baz as a child of bar
		bazHier.Spec.Parent = barName
		updateHierarchy(ctx, bazHier)

		// Verify all labels on baz after set bar as parent
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, barName+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, bazName, keyName)).Should(Equal(valueName))

		// Remove parent from baz
		bazHier.Spec.Parent = ""
		updateHierarchy(ctx, bazHier)

		// Verify all labels on baz after parent removed
		Eventually(getLabel(ctx, bazName, bazName+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, bazName, barName+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, bazName, keyName)).Should(Equal(valueName))
	})

	It("should clear tree labels that are involved in a cycle, except the first one", func() {
		// Create the initial tree:
		// a(0) -+- b(1) -+- d(3) --- f(5)
		//       +- c(2)  +- e(4)
		nms := createNSes(ctx, 6)
		setParent(ctx, nms[1], nms[0])
		setParent(ctx, nms[2], nms[0])
		setParent(ctx, nms[3], nms[1])
		setParent(ctx, nms[4], nms[1])
		setParent(ctx, nms[5], nms[3])

		// Check all labels
		Eventually(getLabel(ctx, nms[0], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[1], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[1], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[2], nms[2]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[2], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[3], nms[3]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[3], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[3], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, nms[4], nms[4]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[4], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[4], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, nms[5], nms[5]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[5], nms[3]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[5], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, nms[5], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("3"))

		// Create a cycle from a(0) to d(3) and check all labels.
		setParent(ctx, nms[0], nms[3])
		Eventually(getLabel(ctx, nms[0], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[1], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[1], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, nms[2], nms[2]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[2], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[3], nms[3]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[3], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, nms[3], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, nms[4], nms[4]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[4], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[4], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, nms[5], nms[5]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[5], nms[3]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[5], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal(""))
		Eventually(getLabel(ctx, nms[5], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal(""))

		// Fix the cycle and ensure that everything's restored
		setParent(ctx, nms[0], "")
		Eventually(getLabel(ctx, nms[0], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[1], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[1], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[2], nms[2]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[2], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[3], nms[3]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[3], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[3], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, nms[4], nms[4]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[4], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[4], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, nms[5], nms[5]+api.LabelTreeDepthSuffix)).Should(Equal("0"))
		Eventually(getLabel(ctx, nms[5], nms[3]+api.LabelTreeDepthSuffix)).Should(Equal("1"))
		Eventually(getLabel(ctx, nms[5], nms[1]+api.LabelTreeDepthSuffix)).Should(Equal("2"))
		Eventually(getLabel(ctx, nms[5], nms[0]+api.LabelTreeDepthSuffix)).Should(Equal("3"))
	})

	It("should remove included-namespace namespace labels from excluded namespaces", func() {
		config.ExcludedNamespaces = map[string]bool{"kube-system": true}
		kubeSystem := getNamespace(ctx, "kube-system")

		// Add additional label "other:other" to verify the labels are updated.
		l := map[string]string{api.LabelIncludedNamespace: "true", "other": "other"}
		kubeSystem.SetLabels(l)
		updateNamespace(ctx, kubeSystem)
		// Verify the labels are updated on the namespace.
		Eventually(getLabel(ctx, "kube-system", "other")).Should(Equal("other"))
		// Verify the included-namespace label is removed by the HC reconciler.
		Eventually(getLabel(ctx, "kube-system", api.LabelIncludedNamespace)).Should(Equal(""))
	})

	It("should set included-namespace namespace labels properly on non-excluded namespaces", func() {
		// Create a namespace without any labels.
		fooName := createNS(ctx, "foo")
		// Verify the label is eventually added by the HC reconciler.
		Eventually(getLabel(ctx, fooName, api.LabelIncludedNamespace)).Should(Equal("true"))

		l := map[string]string{api.LabelIncludedNamespace: "false"}
		// Create a namespace with the label with a wrong value.
		barName := createNSWithLabel(ctx, "bar", l)
		// Verify the label is eventually updated to have the right value.
		Eventually(getLabel(ctx, barName, api.LabelIncludedNamespace)).Should(Equal("true"))
	})
})

func hasCondition(ctx context.Context, nm string, tp, reason string) func() bool {
	return func() bool {
		conds := getHierarchy(ctx, nm).Status.Conditions
		for _, cond := range conds {
			if cond.Type == tp && cond.Reason == reason {
				return true
			}
		}
		return false
	}
}
