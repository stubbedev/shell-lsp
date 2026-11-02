package lint

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	justBase   = nativeBase + 5000
	makeBase   = nativeBase + 6000
	dockerBase = nativeBase + 7000
	nixBase    = nativeBase + 8000
	hostEnd    = nativeBase + 9000
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

const (
	CodeNixSyntax Code = nixBase + 1 + iota
	CodeNixUndefinedVariable
	CodeNixDuplicateAttribute
	CodeNixDuplicateFormal
	CodeNixUnusedLet
	CodeNixUnusedArgument
	CodeNixUnusedFormal
	CodeNixUselessParens
	CodeNixRedundantIf
	CodeNixRedundantRec
	CodeNixSpelledInherit
	CodeNixOrIdentifier
	CodeNixEmptyBinding
	CodeNixUnescapedAntiquotation
	CodeNixBoolComparison
	CodeNixManualInheritFrom
	CodeNixLegacyLet
	CodeNixCollapsibleLet
	CodeNixEtaReduction
	CodeNixEmptyPattern
	CodeNixRedundantPatternBind
	CodeNixUnquotedURI
	CodeNixDeprecatedToPath
	CodeNixBoolSimplification
	CodeNixUselessHasAttr
	CodeNixRepeatedKeys
	CodeNixEmptyListConcat
	CodeNixEmptyInherit
	CodeNixUnusedUnderscore
	CodeNixUsedUnderscore
	CodeNixImpureInFlake
	CodeNixUnpinnedFetch
	CodeNixRecOverride
	CodeNixModuleRecursion
	CodeNixTopLevelWith
	CodeNixStoreCopy
	CodeNixPkgsLib
	CodeNixUnusedFlakeInput
	CodeNixFollowsMissingInput
	CodeNixMissingNixpkgsFollows
)

func (c Code) Host() bool { return c > justBase && c < hostEnd }

func (c Code) Just() bool { return c > justBase && c < makeBase }

func (c Code) Make() bool { return c > makeBase && c < dockerBase }

func (c Code) Docker() bool { return c > dockerBase && c < nixBase }

func (c Code) Nix() bool { return c > nixBase && c < hostEnd }

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

		CodeNixSyntax:                 {severity: SeverityError},
		CodeNixUndefinedVariable:      {severity: SeverityError},
		CodeNixDuplicateAttribute:     {severity: SeverityError},
		CodeNixDuplicateFormal:        {severity: SeverityError},
		CodeNixUnusedLet:              {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixUnusedArgument:         {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixUnusedFormal:           {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixUselessParens:          {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixRedundantIf:            {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixRedundantRec:           {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixSpelledInherit:         {severity: SeverityInfo},
		CodeNixOrIdentifier:           {severity: SeverityWarning},
		CodeNixEmptyBinding:           {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixUnescapedAntiquotation: {severity: SeverityInfo, optional: optionalUnescapedAntiquotations},

		CodeNixBoolComparison:        {severity: SeverityInfo},
		CodeNixManualInheritFrom:     {severity: SeverityInfo},
		CodeNixLegacyLet:             {severity: SeverityWarning, tag: TagDeprecated},
		CodeNixCollapsibleLet:        {severity: SeverityInfo},
		CodeNixEtaReduction:          {severity: SeverityInfo, optional: optionalEtaReduction},
		CodeNixEmptyPattern:          {severity: SeverityInfo, optional: optionalEmptyPattern},
		CodeNixRedundantPatternBind:  {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixUnquotedURI:           {severity: SeverityInfo, tag: TagDeprecated},
		CodeNixDeprecatedToPath:      {severity: SeverityWarning, tag: TagDeprecated},
		CodeNixBoolSimplification:    {severity: SeverityInfo},
		CodeNixUselessHasAttr:        {severity: SeverityInfo},
		CodeNixRepeatedKeys:          {severity: SeverityInfo, optional: optionalRepeatedKeys},
		CodeNixEmptyListConcat:       {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixEmptyInherit:          {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixUnusedUnderscore:      {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixUsedUnderscore:        {severity: SeverityInfo, optional: optionalUsedUnderscore},
		CodeNixImpureInFlake:         {severity: SeverityWarning},
		CodeNixUnpinnedFetch:         {severity: SeverityWarning},
		CodeNixRecOverride:           {severity: SeverityWarning},
		CodeNixModuleRecursion:       {severity: SeverityWarning},
		CodeNixTopLevelWith:          {severity: SeverityInfo, optional: optionalTopLevelWith},
		CodeNixStoreCopy:             {severity: SeverityInfo, optional: optionalStoreCopy},
		CodeNixPkgsLib:               {severity: SeverityInfo, optional: optionalPkgsLib},
		CodeNixUnusedFlakeInput:      {severity: SeverityInfo, tag: TagUnnecessary},
		CodeNixFollowsMissingInput:   {severity: SeverityWarning},
		CodeNixMissingNixpkgsFollows: {severity: SeverityInfo, optional: optionalMissingNixpkgsFollows},
	},
}

type StatixLint struct {
	Name string
	ID   int
	Code Code
}

var statixLints = []StatixLint{
	{"bool_comparison", 1, CodeNixBoolComparison},
	{"empty_let_in", 2, CodeNixEmptyBinding},
	{"manual_inherit", 3, CodeNixSpelledInherit},
	{"manual_inherit_from", 4, CodeNixManualInheritFrom},
	{"legacy_let_syntax", 5, CodeNixLegacyLet},
	{"collapsible_let_in", 6, CodeNixCollapsibleLet},
	{"eta_reduction", 7, CodeNixEtaReduction},
	{"useless_parens", 8, CodeNixUselessParens},
	{"empty_pattern", 10, CodeNixEmptyPattern},
	{"redundant_pattern_bind", 11, CodeNixRedundantPatternBind},
	{"unquoted_uri", 12, CodeNixUnquotedURI},
	{"empty_inherit", 14, CodeNixEmptyInherit},
	{"deprecated_to_path", 17, CodeNixDeprecatedToPath},
	{"bool_simplification", 18, CodeNixBoolSimplification},
	{"useless_has_attr", 19, CodeNixUselessHasAttr},
	{"repeated_keys", 20, CodeNixRepeatedKeys},
	{"empty_list_concat", 23, CodeNixEmptyListConcat},
}

func StatixLints() []StatixLint { return slices.Clone(statixLints) }

func StatixCodes() CodeSet {
	codes := make([]Code, len(statixLints))
	for i, l := range statixLints {
		codes[i] = l.Code
	}
	return CodesOf(codes...)
}

func DeadnixCodes() CodeSet {
	return CodesOf(CodeNixUnusedLet, CodeNixUnusedArgument, CodeNixUnusedFormal, CodeNixUnusedUnderscore, CodeNixUsedUnderscore)
}

func HostFinding(code Code, span parser.Span, message string) Finding {
	m, ok := catalog[code]
	if !ok || !code.Host() && !code.Hadolint() {
		panic("lint: " + code.String() + " is not a host language check")
	}
	return Finding{Code: code, Severity: m.severity, Tag: m.tag, Span: span, Message: message}
}

func SortHostFindings(findings []Finding) { sortFindings(findings) }

func (p Profile) KeepsHost(f Finding) bool { return p.keeps(f, catalog[f.Code], false) }
