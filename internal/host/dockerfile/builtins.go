package dockerfile

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

type spec struct {
	signature string
	doc       string
	flags     []string
	json      bool
	shell     bool
	heredoc   bool
}

var instructions = map[string]spec{
	"ADD":         {signature: "ADD [--chown=user] [--chmod=mode] [--link] [--checksum=sum] [--keep-git-dir] [--unpack] [--exclude=pattern] src... dest", doc: "Copy files, URLs or Git repositories into the image, unpacking local archives.", flags: []string{"chown", "chmod", "link", "keep-git-dir", "checksum", "exclude", "unpack"}, json: true, heredoc: true},
	"ARG":         {signature: "ARG name[=default] ...", doc: "Declare a build argument; before the first `FROM` it is usable in `FROM` lines, inside a stage from this line on."},
	"CMD":         {signature: "CMD command | CMD [\"executable\", \"arg\"...]", doc: "The default command of a container started from the image; the shell form runs through the `SHELL`.", json: true, shell: true},
	"COPY":        {signature: "COPY [--from=stage] [--chown=user] [--chmod=mode] [--link] [--parents] [--exclude=pattern] src... dest", doc: "Copy files from the build context, a stage or an image into the image.", flags: []string{"from", "chown", "chmod", "link", "parents", "exclude"}, json: true, heredoc: true},
	"ENTRYPOINT":  {signature: "ENTRYPOINT command | ENTRYPOINT [\"executable\", \"arg\"...]", doc: "The executable a container runs; the shell form runs through the `SHELL` and ignores `CMD`.", json: true, shell: true},
	"ENV":         {signature: "ENV key=value ...", doc: "Set environment variables for the rest of the stage, its `RUN` commands and containers of the image."},
	"EXPOSE":      {signature: "EXPOSE port[/protocol] ...", doc: "Document the ports a container listens on."},
	"FROM":        {signature: "FROM [--platform=platform] image [AS name]", doc: "Start a build stage from an image or an earlier stage.", flags: []string{"platform"}},
	"HEALTHCHECK": {signature: "HEALTHCHECK [--interval=d] [--timeout=d] [--start-period=d] [--start-interval=d] [--retries=n] CMD command | HEALTHCHECK NONE", doc: "How to check that a container still works.", flags: []string{"interval", "timeout", "start-period", "start-interval", "retries"}},
	"LABEL":       {signature: "LABEL key=value ...", doc: "Add metadata to the image."},
	"MAINTAINER":  {signature: "MAINTAINER name", doc: "The author of the image; deprecated in favour of `LABEL org.opencontainers.image.authors=...`."},
	"ONBUILD":     {signature: "ONBUILD instruction", doc: "An instruction that runs when another build uses this image as its base."},
	"RUN":         {signature: "RUN [--mount=...] [--network=mode] [--security=mode] [--device=name] command | RUN [\"executable\", \"arg\"...]", doc: "Run a command while building; the shell form runs through the `SHELL`.", flags: []string{"mount", "network", "security", "device"}, json: true, shell: true, heredoc: true},
	"SHELL":       {signature: "SHELL [\"executable\", \"arg\"...]", doc: "The shell that runs the shell forms of later `RUN`, `CMD` and `ENTRYPOINT` instructions in the stage.", json: true},
	"STOPSIGNAL":  {signature: "STOPSIGNAL signal", doc: "The signal that stops a container."},
	"USER":        {signature: "USER user[:group]", doc: "The user that runs the rest of the stage and containers of the image."},
	"VOLUME":      {signature: "VOLUME path ... | VOLUME [\"path\"...]", doc: "Mark paths as volumes.", json: true},
	"WORKDIR":     {signature: "WORKDIR path", doc: "The working directory for the rest of the stage."},
}

var flagDocs = map[string]string{
	"chown":          "The owner of the copied files, as `user[:group]`.",
	"chmod":          "The permissions of the copied files.",
	"link":           "Copy into an independent layer that survives changes to earlier layers.",
	"keep-git-dir":   "Keep the `.git` directory of a Git source.",
	"checksum":       "The checksum a remote source must have.",
	"exclude":        "A pattern of paths to leave out.",
	"unpack":         "Whether to unpack archives.",
	"from":           "The stage, image or named context to copy from.",
	"parents":        "Keep the parent directories of the sources.",
	"platform":       "The platform of the image, such as `linux/amd64`.",
	"interval":       "How long to wait between checks.",
	"timeout":        "How long a check may take.",
	"start-period":   "How long a container may take to start before failed checks count.",
	"start-interval": "How long to wait between checks while the container starts.",
	"retries":        "How many failed checks in a row make the container unhealthy.",
	"mount":          "A filesystem to mount for the command: `type=cache`, `bind`, `secret`, `ssh` or `tmpfs`.",
	"network":        "The network the command runs in: `default`, `none` or `host`.",
	"security":       "`insecure` runs the command with elevated privileges.",
	"device":         "A device to expose to the command.",
}

const buildkitArgPrefix = "BUILDKIT_"

const defaultPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

var predefinedArgs = map[string]string{
	"TARGETPLATFORM": "The platform being built for, such as `linux/arm64/v8`.",
	"TARGETOS":       "The operating system of the platform being built for.",
	"TARGETARCH":     "The architecture of the platform being built for.",
	"TARGETVARIANT":  "The variant of the architecture being built for.",
	"BUILDPLATFORM":  "The platform of the machine running the build.",
	"BUILDOS":        "The operating system of the machine running the build.",
	"BUILDARCH":      "The architecture of the machine running the build.",
	"BUILDVARIANT":   "The variant of the architecture of the machine running the build.",
	"TARGETSTAGE":    "The stage the build targets.",
	"HTTP_PROXY":     "A proxy for HTTP requests during the build.",
	"HTTPS_PROXY":    "A proxy for HTTPS requests during the build.",
	"FTP_PROXY":      "A proxy for FTP requests during the build.",
	"NO_PROXY":       "Hosts the build reaches without a proxy.",
	"ALL_PROXY":      "A proxy for every request during the build.",
}

var Builtins = func() model.Builtins {
	var keywords, flags, args []model.Builtin
	for name, s := range instructions {
		keywords = append(keywords, model.Builtin{Name: name, Kind: model.KindKeyword, Signature: s.signature, Doc: s.doc, Arity: model.NoArity})
	}
	for name, doc := range flagDocs {
		var takes []string
		for instruction, s := range instructions {
			if slices.Contains(s.flags, name) {
				takes = append(takes, "`"+instruction+"`")
			}
		}
		slices.Sort(takes)
		flags = append(flags, model.Builtin{Name: name, Kind: model.KindAttribute, Signature: "--" + name, Doc: doc + " For " + strings.Join(takes, ", ") + ".", Arity: model.NoArity})
	}
	for name, doc := range predefinedArgs {
		args = append(args, model.Builtin{Name: name, Kind: model.KindVariable, Signature: "ARG " + name, Doc: doc + " Declare it with `ARG " + name + "` to use it in a stage.", Arity: model.NoArity})
	}
	args = append(args, model.Builtin{Name: "PATH", Kind: model.KindVariable, Signature: "ENV PATH=" + defaultPath, Doc: "The search path BuildKit sets in every stage, unless the base image sets its own.", Arity: model.NoArity})
	return model.NewBuiltins(keywords, flags, args)
}()
