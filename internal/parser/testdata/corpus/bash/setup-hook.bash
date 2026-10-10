# setup hook in the style of build system hooks
declare -gA seenPaths=()
preConfigureHooks+=(addPkgConfigPath)

addToSearchPathWithCustomDelimiter() {
	local delimiter="$1"
	local varName="$2"
	local dir="$3"
	if [[ -d "$dir" && "${!varName:+${delimiter}${!varName}${delimiter}}" != *"${delimiter}${dir}${delimiter}"* ]]; then
		export "${varName}=${!varName:+${!varName}${delimiter}}${dir}"
	fi
}

addPkgConfigPath() {
	local pkg
	for pkg in "${pkgsHostTarget[@]}"; do
		if [[ -n "${seenPaths[$pkg]-}" ]]; then
			continue
		fi
		seenPaths[$pkg]=1
		addToSearchPathWithCustomDelimiter : PKG_CONFIG_PATH "$pkg/lib/pkgconfig"
	done
}

runHook() {
	local hookName="$1"
	shift
	local hooksSlice="${hookName%Hook}Hooks[@]"
	local hook
	for hook in "_callImplicitHook 0 $hookName" ${!hooksSlice+"${!hooksSlice}"}; do
		_eval "$hook" "$@"
	done
	return 0
}
