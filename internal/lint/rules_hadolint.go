package lint

const (
	DL1001 Code = hadolintBase + 1001
	DL3001 Code = hadolintBase + 3001
	DL3002 Code = hadolintBase + 3002
	DL3003 Code = hadolintBase + 3003
	DL3004 Code = hadolintBase + 3004
	DL3006 Code = hadolintBase + 3006
	DL3007 Code = hadolintBase + 3007
	DL3008 Code = hadolintBase + 3008
	DL3009 Code = hadolintBase + 3009
	DL3010 Code = hadolintBase + 3010
	DL3011 Code = hadolintBase + 3011
	DL3013 Code = hadolintBase + 3013
	DL3014 Code = hadolintBase + 3014
	DL3015 Code = hadolintBase + 3015
	DL3016 Code = hadolintBase + 3016
	DL3018 Code = hadolintBase + 3018
	DL3019 Code = hadolintBase + 3019
	DL3020 Code = hadolintBase + 3020
	DL3021 Code = hadolintBase + 3021
	DL3026 Code = hadolintBase + 3026
	DL3027 Code = hadolintBase + 3027
	DL3028 Code = hadolintBase + 3028
	DL3030 Code = hadolintBase + 3030
	DL3032 Code = hadolintBase + 3032
	DL3033 Code = hadolintBase + 3033
	DL3034 Code = hadolintBase + 3034
	DL3035 Code = hadolintBase + 3035
	DL3036 Code = hadolintBase + 3036
	DL3037 Code = hadolintBase + 3037
	DL3038 Code = hadolintBase + 3038
	DL3040 Code = hadolintBase + 3040
	DL3041 Code = hadolintBase + 3041
	DL3042 Code = hadolintBase + 3042
	DL3044 Code = hadolintBase + 3044
	DL3045 Code = hadolintBase + 3045
	DL3046 Code = hadolintBase + 3046
	DL3047 Code = hadolintBase + 3047
	DL3048 Code = hadolintBase + 3048
	DL3049 Code = hadolintBase + 3049
	DL3050 Code = hadolintBase + 3050
	DL3051 Code = hadolintBase + 3051
	DL3052 Code = hadolintBase + 3052
	DL3053 Code = hadolintBase + 3053
	DL3054 Code = hadolintBase + 3054
	DL3055 Code = hadolintBase + 3055
	DL3056 Code = hadolintBase + 3056
	DL3057 Code = hadolintBase + 3057
	DL3058 Code = hadolintBase + 3058
	DL3059 Code = hadolintBase + 3059
	DL3060 Code = hadolintBase + 3060
	DL3062 Code = hadolintBase + 3062
	DL4001 Code = hadolintBase + 4001
	DL4005 Code = hadolintBase + 4005
	DL4006 Code = hadolintBase + 4006
)

var hadolintFamily = family{
	codes: map[Code]meta{
		DL1001: {severity: SeverityInfo, optional: optionalNoIgnorePragmas, summary: "A `# hadolint ignore=` comment hides findings; the project asks for none."},
		DL3001: {severity: SeverityInfo, summary: "RUN starts a tool meant for a running system, such as an editor or process viewer, which is no use while building."},
		DL3002: {severity: SeverityWarning, summary: "The last USER of a stage is root, so the container runs with full privileges."},
		DL3003: {severity: SeverityWarning, summary: "RUN changes directory with cd; WORKDIR sets the directory for every later instruction and is easier to follow."},
		DL3004: {severity: SeverityError, summary: "RUN calls sudo, which builds already run as root do not need and which images rarely have configured."},
		DL3006: {severity: SeverityWarning, summary: "FROM names an image without a tag or digest, so each build may start from a different image."},
		DL3007: {severity: SeverityWarning, summary: "FROM uses the latest tag, which moves, so each build may start from a different image."},
		DL3008: {severity: SeverityWarning, summary: "apt-get install lists a package without a version, so a rebuild may install a different one."},
		DL3009: {severity: SeverityInfo, summary: "The package lists apt-get update downloads are left in the image."},
		DL3010: {severity: SeverityInfo, summary: "An archive is copied in and then unpacked; ADD unpacks a local archive itself, in one layer."},
		DL3011: {severity: SeverityError, summary: "EXPOSE names a port above 65535, which no protocol can use."},
		DL3013: {severity: SeverityWarning, summary: "pip install lists a package without a version, so a rebuild may install a different one."},
		DL3014: {severity: SeverityWarning, summary: "apt-get would stop to ask for confirmation, which fails a build; it needs -y."},
		DL3015: {severity: SeverityInfo, summary: "apt-get install also installs recommended packages unless --no-install-recommends is given."},
		DL3016: {severity: SeverityWarning, summary: "npm install lists a package without a version, so a rebuild may install a different one."},
		DL3018: {severity: SeverityWarning, summary: "apk add lists a package without a version, so a rebuild may install a different one."},
		DL3019: {severity: SeverityInfo, summary: "apk add keeps its package index in the image unless --no-cache is given."},
		DL3020: {severity: SeverityError, summary: "ADD copies plain files or folders; COPY does that without ADD's URL and archive handling."},
		DL3021: {severity: SeverityError, summary: "COPY with several sources needs a destination that ends in /."},
		DL3026: {severity: SeverityError, summary: "FROM pulls an image from a registry that is not on the configured list of trusted registries."},
		DL3027: {severity: SeverityWarning, summary: "RUN uses apt, whose interface is meant for people and may change; apt-get and apt-cache are stable."},
		DL3028: {severity: SeverityWarning, summary: "gem install lists a gem without a version, so a rebuild may install a different one."},
		DL3030: {severity: SeverityWarning, summary: "yum would stop to ask for confirmation, which fails a build; it needs -y."},
		DL3032: {severity: SeverityWarning, summary: "The packages and metadata yum downloads are left in the image without yum clean all."},
		DL3033: {severity: SeverityWarning, summary: "yum install lists a package without a version, so a rebuild may install a different one."},
		DL3034: {severity: SeverityWarning, summary: "zypper would stop to ask for confirmation, which fails a build; it needs --non-interactive."},
		DL3035: {severity: SeverityWarning, summary: "zypper dist-upgrade upgrades the whole base image; a newer base image does that reproducibly."},
		DL3036: {severity: SeverityWarning, summary: "The packages and metadata zypper downloads are left in the image without zypper clean."},
		DL3037: {severity: SeverityWarning, summary: "zypper install lists a package without a version, so a rebuild may install a different one."},
		DL3038: {severity: SeverityWarning, summary: "dnf would stop to ask for confirmation, which fails a build; it needs -y."},
		DL3040: {severity: SeverityWarning, summary: "The packages and metadata dnf downloads are left in the image without dnf clean all."},
		DL3041: {severity: SeverityWarning, summary: "dnf install lists a package without a version, so a rebuild may install a different one."},
		DL3042: {severity: SeverityWarning, summary: "pip install keeps the packages it downloads in a cache inside the image unless --no-cache-dir is given."},
		DL3044: {severity: SeverityError, summary: "ENV reads a variable that the same ENV sets, which Docker expands to its earlier value."},
		DL3045: {severity: SeverityWarning, summary: "COPY copies to a relative path while no WORKDIR is set, so the base image decides where it lands."},
		DL3046: {severity: SeverityWarning, summary: "useradd with a large UID and without -l writes a huge sparse lastlog file into the image."},
		DL3047: {severity: SeverityInfo, summary: "wget prints a progress bar line by line into the build log unless told to be quieter."},
		DL3048: {severity: SeverityStyle, summary: "A label key breaks Docker's naming guidelines for label keys."},
		DL3049: {severity: SeverityInfo, summary: "The image lacks a label the configured label schema requires."},
		DL3050: {severity: SeverityInfo, summary: "A label is not in the configured label schema, which is strict."},
		DL3051: {severity: SeverityWarning, summary: "A label the label schema names is empty."},
		DL3052: {severity: SeverityWarning, summary: "A label the label schema types as url is not an absolute URL."},
		DL3053: {severity: SeverityWarning, summary: "A label the label schema types as rfc3339 is not an RFC 3339 date and time."},
		DL3054: {severity: SeverityWarning, summary: "A label the label schema types as spdx is not an SPDX license expression."},
		DL3055: {severity: SeverityWarning, summary: "A label the label schema types as hash is not a git commit hash."},
		DL3056: {severity: SeverityWarning, summary: "A label the label schema types as semver is not a semantic version."},
		DL3057: {severity: SeverityInfo, optional: optionalRequireHealthcheck, summary: "A stage has no HEALTHCHECK, so Docker cannot tell whether its container works."},
		DL3058: {severity: SeverityWarning, summary: "A label the label schema types as email is not an email address."},
		DL3059: {severity: SeverityInfo, summary: "Consecutive RUN instructions each add a layer; joining them with && makes one."},
		DL3060: {severity: SeverityInfo, summary: "The yarn cache is left in the image without yarn cache clean."},
		DL3062: {severity: SeverityWarning, summary: "go install names a module without a version, so a rebuild may build a different one."},
		DL4001: {severity: SeverityWarning, summary: "A stage downloads with both wget and curl; one tool is enough."},
		DL4005: {severity: SeverityWarning, summary: "RUN replaces /bin/sh with a link; SHELL changes the shell of later instructions without touching the image."},
		DL4006: {severity: SeverityWarning, summary: "A pipeline in RUN succeeds when its last command does, hiding a failure earlier in the pipe, unless pipefail is set."},
	},
}
