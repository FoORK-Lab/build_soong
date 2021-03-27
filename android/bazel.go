// Copyright 2021 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package android

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

type bazelModuleProperties struct {
	// The label of the Bazel target replacing this Soong module. When run in conversion mode, this
	// will import the handcrafted build target into the autogenerated file. Note: this may result in
	// a conflict due to duplicate targets if bp2build_available is also set.
	Label *string

	// If true, bp2build will generate the converted Bazel target for this module. Note: this may
	// cause a conflict due to the duplicate targets if label is also set.
	//
	// This is a bool pointer to support tristates: true, false, not set.
	//
	// To opt-in a module, set bazel_module: { bp2build_available: true }
	// To opt-out a module, set bazel_module: { bp2build_available: false }
	// To defer the default setting for the directory, do not set the value.
	Bp2build_available *bool
}

// Properties contains common module properties for Bazel migration purposes.
type properties struct {
	// In USE_BAZEL_ANALYSIS=1 mode, this represents the Bazel target replacing
	// this Soong module.
	Bazel_module bazelModuleProperties
}

// BazelModuleBase contains the property structs with metadata for modules which can be converted to
// Bazel.
type BazelModuleBase struct {
	bazelProperties properties
}

// Bazelable is specifies the interface for modules that can be converted to Bazel.
type Bazelable interface {
	bazelProps() *properties
	HasHandcraftedLabel() bool
	HandcraftedLabel() string
	GetBazelLabel(ctx BazelConversionPathContext, module blueprint.Module) string
	ConvertWithBp2build(ctx BazelConversionPathContext) bool
	GetBazelBuildFileContents(c Config, path, name string) (string, error)
	ConvertedToBazel(ctx BazelConversionPathContext) bool
}

// BazelModule is a lightweight wrapper interface around Module for Bazel-convertible modules.
type BazelModule interface {
	Module
	Bazelable
}

// InitBazelModule is a wrapper function that decorates a BazelModule with Bazel-conversion
// properties.
func InitBazelModule(module BazelModule) {
	module.AddProperties(module.bazelProps())
}

// bazelProps returns the Bazel properties for the given BazelModuleBase.
func (b *BazelModuleBase) bazelProps() *properties {
	return &b.bazelProperties
}

// HasHandcraftedLabel returns whether this module has a handcrafted Bazel label.
func (b *BazelModuleBase) HasHandcraftedLabel() bool {
	return b.bazelProperties.Bazel_module.Label != nil
}

// HandcraftedLabel returns the handcrafted label for this module, or empty string if there is none
func (b *BazelModuleBase) HandcraftedLabel() string {
	return proptools.String(b.bazelProperties.Bazel_module.Label)
}

// GetBazelLabel returns the Bazel label for the given BazelModuleBase.
func (b *BazelModuleBase) GetBazelLabel(ctx BazelConversionPathContext, module blueprint.Module) string {
	if b.HasHandcraftedLabel() {
		return b.HandcraftedLabel()
	}
	if b.ConvertWithBp2build(ctx) {
		return bp2buildModuleLabel(ctx, module)
	}
	return "" // no label for unconverted module
}

// Configuration to decide if modules in a directory should default to true/false for bp2build_available
type Bp2BuildConfig map[string]BazelConversionConfigEntry
type BazelConversionConfigEntry int

const (
	// iota + 1 ensures that the int value is not 0 when used in the Bp2buildAllowlist map,
	// which can also mean that the key doesn't exist in a lookup.

	// all modules in this package and subpackages default to bp2build_available: true.
	// allows modules to opt-out.
	Bp2BuildDefaultTrueRecursively BazelConversionConfigEntry = iota + 1

	// all modules in this package (not recursively) default to bp2build_available: false.
	// allows modules to opt-in.
	Bp2BuildDefaultFalse
)

var (
	// Configure modules in these directories to enable bp2build_available: true or false by default.
	bp2buildDefaultConfig = Bp2BuildConfig{
		"bionic":                Bp2BuildDefaultTrueRecursively,
		"system/core/libcutils": Bp2BuildDefaultTrueRecursively,
		"system/logging/liblog": Bp2BuildDefaultTrueRecursively,
	}

	// Per-module denylist to always opt modules out.
	bp2buildModuleDoNotConvert = map[string]bool{
		"libBionicBenchmarksUtils":      true,
		"libbionic_spawn_benchmark":     true,
		"libc_jemalloc_wrapper":         true,
		"libc_bootstrap":                true,
		"libc_init_static":              true,
		"libc_init_dynamic":             true,
		"libc_tzcode":                   true,
		"libc_freebsd":                  true,
		"libc_freebsd_large_stack":      true,
		"libc_netbsd":                   true,
		"libc_openbsd_ndk":              true,
		"libc_openbsd_large_stack":      true,
		"libc_openbsd":                  true,
		"libc_gdtoa":                    true,
		"libc_fortify":                  true,
		"libc_bionic":                   true,
		"libc_bionic_ndk":               true,
		"libc_bionic_systrace":          true,
		"libc_pthread":                  true,
		"libc_syscalls":                 true,
		"libc_aeabi":                    true,
		"libc_ndk":                      true,
		"libc_nopthread":                true,
		"libc_common":                   true,
		"libc_static_dispatch":          true,
		"libc_dynamic_dispatch":         true,
		"libc_common_static":            true,
		"libc_common_shared":            true,
		"libc_unwind_static":            true,
		"libc_nomalloc":                 true,
		"libasync_safe":                 true,
		"libc_malloc_debug_backtrace":   true,
		"libsystemproperties":           true,
		"libdl_static":                  true,
		"liblinker_main":                true,
		"liblinker_malloc":              true,
		"liblinker_debuggerd_stub":      true,
		"libbionic_tests_headers_posix": true,
		"libc_dns":                      true,
	}
)

// ConvertWithBp2build returns whether the given BazelModuleBase should be converted with bp2build.
func (b *BazelModuleBase) ConvertWithBp2build(ctx BazelConversionPathContext) bool {
	if bp2buildModuleDoNotConvert[ctx.Module().Name()] {
		return false
	}

	// Ensure that the module type of this module has a bp2build converter. This
	// prevents mixed builds from using auto-converted modules just by matching
	// the package dir; it also has to have a bp2build mutator as well.
	if ctx.Config().bp2buildModuleTypeConfig[ctx.ModuleType()] == false {
		return false
	}

	packagePath := ctx.ModuleDir()
	config := ctx.Config().bp2buildPackageConfig

	// This is a tristate value: true, false, or unset.
	propValue := b.bazelProperties.Bazel_module.Bp2build_available
	if bp2buildDefaultTrueRecursively(packagePath, config) {
		// Allow modules to explicitly opt-out.
		return proptools.BoolDefault(propValue, true)
	}

	// Allow modules to explicitly opt-in.
	return proptools.BoolDefault(propValue, false)
}

// bp2buildDefaultTrueRecursively checks that the package contains a prefix from the
// set of package prefixes where all modules must be converted. That is, if the
// package is x/y/z, and the list contains either x, x/y, or x/y/z, this function will
// return true.
//
// However, if the package is x/y, and it matches a Bp2BuildDefaultFalse "x/y" entry
// exactly, this module will return false early.
//
// This function will also return false if the package doesn't match anything in
// the config.
func bp2buildDefaultTrueRecursively(packagePath string, config Bp2BuildConfig) bool {
	ret := false

	if config[packagePath] == Bp2BuildDefaultFalse {
		return false
	}

	packagePrefix := ""
	// e.g. for x/y/z, iterate over x, x/y, then x/y/z, taking the final value from the allowlist.
	for _, part := range strings.Split(packagePath, "/") {
		packagePrefix += part
		if config[packagePrefix] == Bp2BuildDefaultTrueRecursively {
			// package contains this prefix and this prefix should convert all modules
			return true
		}
		// Continue to the next part of the package dir.
		packagePrefix += "/"
	}

	return ret
}

// GetBazelBuildFileContents returns the file contents of a hand-crafted BUILD file if available or
// an error if there are errors reading the file.
// TODO(b/181575318): currently we append the whole BUILD file, let's change that to do
// something more targeted based on the rule type and target.
func (b *BazelModuleBase) GetBazelBuildFileContents(c Config, path, name string) (string, error) {
	if !strings.Contains(b.HandcraftedLabel(), path) {
		return "", fmt.Errorf("%q not found in bazel_module.label %q", path, b.HandcraftedLabel())
	}
	name = filepath.Join(path, name)
	f, err := c.fs.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(data[:]), nil
}

// ConvertedToBazel returns whether this module has been converted to Bazel, whether automatically
// or manually
func (b *BazelModuleBase) ConvertedToBazel(ctx BazelConversionPathContext) bool {
	return b.ConvertWithBp2build(ctx) || b.HasHandcraftedLabel()
}