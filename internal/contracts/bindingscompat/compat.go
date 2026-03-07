// Package bindingscompat provides types that generated contract bindings (cre generate-bindings evm)
// expect but that are not present in the current cre-sdk-go evm/bindings package. The engine uses
// evm.LogTrigger and evm.FilterLogTriggerRequest directly and does not use these generated trigger
// helpers; this package exists only so the generated code compiles.
package bindingscompat

// BindingOptions is a placeholder for the optional binding options argument in generated New* constructors.
// The current SDK uses evm/bindings.ContractInitOptions; generated code expected BindingOptions.
type BindingOptions struct{}

// LogTrigger is a placeholder return type for generated LogTrigger* methods. The workflow uses
// evm.LogTrigger(chainSelector, &evm.FilterLogTriggerRequest{...}) directly, not these methods.
type LogTrigger[T any] struct{}
