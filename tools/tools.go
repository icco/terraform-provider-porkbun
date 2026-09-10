//go:build generate

// Package tools pins the documentation generator. It is a separate Go module
// so that tfplugindocs' dependency tree never lands in the provider's go.sum.
package tools

import (
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)

// Format the Terraform examples that feed the docs, then generate the docs.
//go:generate terraform fmt -recursive ../examples/
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. --provider-name porkbun
