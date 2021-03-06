/*
Copyright 2016 The Kubernetes Authors.

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

package generators

import (
	"github.com/n3wscott/kn-code-generators/cmd/injection-gen/generators/util"
	"io"
	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"

	clientgentypes "github.com/n3wscott/kn-code-generators/cmd/injection-gen/types"
	codegennamer "github.com/n3wscott/kn-code-generators/pkg/namer"

	"k8s.io/klog"
)

// injectionTestGenerator produces a file of listers for a given GroupVersion and
// type.
type injectionGenerator struct {
	generator.DefaultGen
	outputPackage               string
	groupPkgName                string
	groupVersion                clientgentypes.GroupVersion
	groupGoName                 string
	typeToGenerate              *types.Type
	imports                     namer.ImportTracker
	clientSetPackage            string
	internalInterfacesPackage   string
	typedInformerPackage        string
	groupInformerFactoryPackage string
}

var _ generator.Generator = &injectionGenerator{}

func (g *injectionGenerator) Filter(c *generator.Context, t *types.Type) bool {
	return t == g.typeToGenerate
}

func (g *injectionGenerator) Namers(c *generator.Context) namer.NameSystems {
	pluralExceptions := map[string]string{
		"Endpoints": "Endpoints",
	}

	lowercaseNamer := namer.NewAllLowercasePluralNamer(pluralExceptions)

	publicPluralNamer := &ExceptionNamer{
		Exceptions: map[string]string{
			// these exceptions are used to deconflict the generated code
			// you can put your fully qualified package like
			// to generate a name that doesn't conflict with your group.
			// "k8s.io/apis/events/v1beta1.Event": "EventResource"
		},
		KeyFunc: func(t *types.Type) string {
			klog.Info()
			return t.Name.Package + "." + t.Name.Name + t.Name.Name
		},
		Delegate: namer.NewPublicPluralNamer(pluralExceptions),
	}

	return namer.NameSystems{
		"raw":          namer.NewRawNamer(g.outputPackage, g.imports),
		"publicPlural": publicPluralNamer,
		"resource":     codegennamer.NewTagOverrideNamer("resourceName", lowercaseNamer),
	}
}

// ExceptionNamer allows you specify exceptional cases with exact names.  This allows you to have control
// for handling various conflicts, like group and resource names for instance.
type ExceptionNamer struct {
	Exceptions map[string]string
	KeyFunc    func(*types.Type) string

	Delegate namer.Namer
}

// Name provides the requested name for a type.
func (n *ExceptionNamer) Name(t *types.Type) string {
	key := n.KeyFunc(t)
	if exception, ok := n.Exceptions[key]; ok {
		return exception
	}
	return n.Delegate.Name(t)
}

func (g *injectionGenerator) Imports(c *generator.Context) (imports []string) {
	imports = append(imports, g.imports.ImportLines()...)
	return
}

func (g *injectionGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "{{", "}}")

	klog.V(5).Infof("processing type %v", t)

	klog.V(5).Infof("XXX %v\n\n%+v", g.clientSetPackage, c)

	clientSetInterface := c.Universe.Type(types.Name{Package: g.clientSetPackage, Name: "Interface"})

	informerFor := "InformerFor" // TODO: rename

	tags, err := util.ParseClientGenTags(append(t.SecondClosestCommentLines, t.CommentLines...))
	if err != nil {
		return err
	}

	m := map[string]interface{}{
		"apiScheme":                       c.Universe.Type(apiScheme),
		"cacheIndexers":                   c.Universe.Type(cacheIndexers),
		"cacheListWatch":                  c.Universe.Type(cacheListWatch),
		"cacheMetaNamespaceIndexFunc":     c.Universe.Function(cacheMetaNamespaceIndexFunc),
		"cacheNamespaceIndex":             c.Universe.Variable(cacheNamespaceIndex),
		"cacheNewSharedIndexInformer":     c.Universe.Function(cacheNewSharedIndexInformer),
		"cacheSharedIndexInformer":        c.Universe.Type(cacheSharedIndexInformer),
		"clientSetInterface":              clientSetInterface,
		"group":                           namer.IC(g.groupGoName),
		"informerFor":                     informerFor,
		"interfacesTweakListOptionsFunc":  c.Universe.Type(types.Name{Package: g.internalInterfacesPackage, Name: "TweakListOptionsFunc"}),
		"interfacesSharedInformerFactory": c.Universe.Type(types.Name{Package: g.internalInterfacesPackage, Name: "SharedInformerFactory"}),
		"listOptions":                     c.Universe.Type(listOptions),
		"namespaceAll":                    c.Universe.Type(metav1NamespaceAll),
		"namespaced":                      !tags.NonNamespaced,
		"runtimeObject":                   c.Universe.Type(runtimeObject),
		"timeDuration":                    c.Universe.Type(timeDuration),
		"type":                            t,
		"v1ListOptions":                   c.Universe.Type(v1ListOptions),
		"version":                         namer.IC(g.groupVersion.Version.String()),
		"resource":                        namer.IC(t.Name.Name),
		"watchInterface":                  c.Universe.Type(watchInterface),
		"injectionRegisterInformer":       c.Universe.Function(types.Name{Package: "github.com/knative/pkg/injection", Name: "RegisterInformer"}),
		"controllerInformer":              c.Universe.Function(types.Name{Package: "github.com/knative/pkg/controller", Name: "Informer"}),
		"informersTypedInformer":          c.Universe.Function(types.Name{Package: g.typedInformerPackage, Name: t.Name.Name + "Informer"}),
		"factoryGet":                      c.Universe.Function(types.Name{Package: g.groupInformerFactoryPackage, Name: "Get"}),
	}

	sw.Do(injectionInformer, m)

	return sw.Error()
}

var injectionInformer = `
func init() {
	{{.injectionRegisterInformer|raw}}(withInformer)
}

// key is used for associating the Informer inside the context.Context.
type Key struct{}

func withInformer(ctx context.Context) (context.Context, {{.controllerInformer|raw}}) {
	f := {{.factoryGet|raw}}(ctx)
	inf := f.{{.group}}().{{.version}}().{{.type|publicPlural}}()
	return context.WithValue(ctx, Key{}, inf), inf.Informer()
}	
		
// Get extracts the typed informer from the context.
func Get(ctx context.Context) {{.informersTypedInformer|raw}} {
	untyped := ctx.Value(Key{})
	if untyped == nil {
		return nil
	}
	return untyped.({{.informersTypedInformer|raw}})
}
`
