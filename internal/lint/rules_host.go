package lint

import "github.com/stubbedev/shell-lsp/internal/parser"

const (
	justBase   = nativeBase + 5000
	makeBase   = nativeBase + 6000
	dockerBase = nativeBase + 7000
	hostEnd    = nativeBase + 8000
)

const (
	CodeJustSyntax Code = justBase + 1 + iota
	CodeJustUnknownRecipe
	CodeJustUnknownVariable
	CodeJustUnknownFunction
	CodeJustFunctionArguments
	CodeJustDependencyArguments
	CodeJustUnknownSetting
	CodeJustUnknownAttribute
	CodeJustDuplicateRecipe
	CodeJustDuplicateVariable
	CodeJustDuplicateAlias
	CodeJustCircular
	CodeJustUnusedParameter
	CodeJustUnusedVariable
	CodeJustDuplicateSetting
	CodeJustSettingValue
	CodeJustUnknownModule
	CodeJustMissingFile
	CodeJustParameterOrder
	CodeJustAttributeArguments
	CodeJustIndentation
	CodeJustDeprecated
	CodeJustNeedsSetting
	CodeJustAbsentModule
)

const (
	CodeMakeSyntax Code = makeBase + 1 + iota
	CodeMakeConditional
	CodeMakeDefine
	CodeMakeRecipePrefix
	CodeMakeRecipeOutsideRule
	CodeMakeUndefinedVariable
	CodeMakeRecursiveVariable
	CodeMakePhonyWithoutRule
	CodeMakeDuplicateRecipe
	CodeMakeUnknownFunction
	CodeMakeFunctionArguments
	CodeMakeUnterminatedReference
	CodeMakeMissingInclude
	CodeMakeUnusedVariable
	CodeMakeDeferredUnterminatedReference
	CodeMakePatternMismatch
	CodeMakeNotPhony
)

const (
	CodeDockerSyntax Code = dockerBase + 1 + iota
	CodeDockerUnknownInstruction
	CodeDockerUnknownFlag
	CodeDockerJSONArgsRecommended
	CodeDockerUnknownStage
	CodeDockerUndefinedVariable
	CodeDockerDuplicateStage
	CodeDockerUnusedArg
	CodeDockerStageNameCasing
	CodeDockerFromAsCasing
	CodeDockerEmptyContinuation
	CodeDockerInstructionCasing
	CodeDockerReservedStageName
	CodeDockerMaintainerDeprecated
	CodeDockerWorkdirRelative
	CodeDockerMultipleInstructions
	CodeDockerLegacyKeyValue
	CodeDockerSecretInArgOrEnv
	CodeDockerExposeProtoCasing
	CodeDockerExposeInvalidFormat
	CodeDockerInvalidDefaultArgInFrom
	CodeDockerConstantPlatform
	CodeDockerRedundantTargetPlatform
	CodeDockerStageUsedBeforeDefinition
)

func (c Code) Host() bool { return c > justBase && c < hostEnd }

func (c Code) Just() bool { return c > justBase && c < makeBase }

func (c Code) Make() bool { return c > makeBase && c < dockerBase }

func (c Code) Docker() bool { return c > dockerBase && c < hostEnd }

var hostFamily = family{
	codes: map[Code]meta{
		CodeJustSyntax:              {severity: SeverityError},
		CodeJustUnknownRecipe:       {severity: SeverityError},
		CodeJustUnknownVariable:     {severity: SeverityError},
		CodeJustUnknownFunction:     {severity: SeverityError},
		CodeJustFunctionArguments:   {severity: SeverityError},
		CodeJustDependencyArguments: {severity: SeverityError},
		CodeJustUnknownSetting:      {severity: SeverityError},
		CodeJustUnknownAttribute:    {severity: SeverityError},
		CodeJustDuplicateRecipe:     {severity: SeverityError},
		CodeJustDuplicateVariable:   {severity: SeverityError},
		CodeJustDuplicateAlias:      {severity: SeverityError},
		CodeJustCircular:            {severity: SeverityError},
		CodeJustUnusedParameter:     {severity: SeverityInfo, tag: TagUnnecessary},
		CodeJustUnusedVariable:      {severity: SeverityInfo, tag: TagUnnecessary},
		CodeJustDuplicateSetting:    {severity: SeverityError},
		CodeJustSettingValue:        {severity: SeverityError},
		CodeJustUnknownModule:       {severity: SeverityError},
		CodeJustMissingFile:         {severity: SeverityError},
		CodeJustParameterOrder:      {severity: SeverityError},
		CodeJustAttributeArguments:  {severity: SeverityError},
		CodeJustIndentation:         {severity: SeverityError},
		CodeJustDeprecated:          {severity: SeverityWarning, tag: TagDeprecated},
		CodeJustNeedsSetting:        {severity: SeverityError},
		CodeJustAbsentModule:        {severity: SeverityWarning},

		CodeMakeSyntax:                {severity: SeverityError},
		CodeMakeConditional:           {severity: SeverityError},
		CodeMakeDefine:                {severity: SeverityError},
		CodeMakeRecipePrefix:          {severity: SeverityError},
		CodeMakeRecipeOutsideRule:     {severity: SeverityError},
		CodeMakeUndefinedVariable:     {severity: SeverityInfo},
		CodeMakeRecursiveVariable:     {severity: SeverityError},
		CodeMakePhonyWithoutRule:      {severity: SeverityWarning},
		CodeMakeDuplicateRecipe:       {severity: SeverityWarning},
		CodeMakeUnknownFunction:       {severity: SeverityWarning},
		CodeMakeFunctionArguments:     {severity: SeverityError},
		CodeMakeUnterminatedReference: {severity: SeverityError},
		CodeMakeMissingInclude:        {severity: SeverityError},
		CodeMakeUnusedVariable:        {severity: SeverityInfo, tag: TagUnnecessary},

		CodeMakeDeferredUnterminatedReference: {severity: SeverityWarning},
		CodeMakePatternMismatch:               {severity: SeverityWarning},
		CodeMakeNotPhony:                      {severity: SeverityWarning},

		CodeDockerSyntax:                    {severity: SeverityError},
		CodeDockerUnknownInstruction:        {severity: SeverityError},
		CodeDockerUnknownFlag:               {severity: SeverityError},
		CodeDockerJSONArgsRecommended:       {severity: SeverityWarning},
		CodeDockerUnknownStage:              {severity: SeverityInfo},
		CodeDockerUndefinedVariable:         {severity: SeverityWarning},
		CodeDockerDuplicateStage:            {severity: SeverityWarning},
		CodeDockerUnusedArg:                 {severity: SeverityInfo, tag: TagUnnecessary},
		CodeDockerStageNameCasing:           {severity: SeverityWarning},
		CodeDockerFromAsCasing:              {severity: SeverityWarning},
		CodeDockerEmptyContinuation:         {severity: SeverityWarning},
		CodeDockerInstructionCasing:         {severity: SeverityWarning},
		CodeDockerReservedStageName:         {severity: SeverityWarning},
		CodeDockerMaintainerDeprecated:      {severity: SeverityWarning, tag: TagDeprecated},
		CodeDockerWorkdirRelative:           {severity: SeverityWarning},
		CodeDockerMultipleInstructions:      {severity: SeverityWarning},
		CodeDockerLegacyKeyValue:            {severity: SeverityWarning},
		CodeDockerSecretInArgOrEnv:          {severity: SeverityWarning},
		CodeDockerExposeProtoCasing:         {severity: SeverityWarning},
		CodeDockerExposeInvalidFormat:       {severity: SeverityWarning},
		CodeDockerInvalidDefaultArgInFrom:   {severity: SeverityWarning},
		CodeDockerConstantPlatform:          {severity: SeverityWarning},
		CodeDockerRedundantTargetPlatform:   {severity: SeverityWarning},
		CodeDockerStageUsedBeforeDefinition: {severity: SeverityError},
	},
}

func HostFinding(code Code, span parser.Span, message string) Finding {
	m, ok := catalog[code]
	if !ok || !code.Host() && !code.Hadolint() {
		panic("lint: " + code.String() + " is not a host language check")
	}
	return Finding{Code: code, Severity: m.severity, Tag: m.tag, Span: span, Message: message}
}
