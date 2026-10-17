package hosttest

import "strings"

var (
	genImages     = []string{"alpine:3.20", "debian:bookworm-slim", "golang:1.23", "scratch", "$BASE", "${BASE}:latest", "node:20-alpine"}
	genStageNames = []string{"build", "test", "final", "Base", "deps"}
	genArgs       = []string{"VERSION", "BASE", "TARGETARCH", "APP_HOME", "USER_ID", "API_TOKEN", "DEBUG"}
	genPaths      = []string{"/app", "/src", "app", ".", "go.mod", "package.json", "/etc/conf.d/", "$APP_HOME", "${APP_HOME}/bin"}
	genCommands   = []string{"apk add --no-cache curl", "go build -o /out/app ./...", "npm ci", "echo \"$VERSION\" > /version", "make && make install", "rm -rf /var/cache/apk/*", "test -n \"$DEBUG\" || exit 0"}
)

func GenerateDockerfile(choices []byte) string {
	c := &chooser{choices: choices}
	var sb strings.Builder
	if c.next(3) == 0 {
		sb.WriteString(c.pick("# syntax=docker/dockerfile:1\n", "# escape=`\n", "# check=skip=JSONArgsRecommended\n"))
	}
	for range c.next(3) {
		sb.WriteString("ARG " + c.pick(genArgs...) + c.pick("", "=1.0", "=alpine:3.20", "=") + "\n")
	}
	for stages := 0; stages == 0 || stages < 4 && !c.done() && c.next(3) != 0; stages++ {
		genStage(c, &sb)
	}
	return sb.String()
}

func genStage(c *chooser, sb *strings.Builder) {
	from := c.pick("FROM", "FROM", "FROM", "from")
	sb.WriteString(from + c.pick("", "", " --platform=$BUILDPLATFORM", " --platform=linux/amd64") + " " + c.pick(genImages...))
	if c.next(2) == 0 {
		sb.WriteString(" " + c.pick("AS", "AS", "as") + " " + c.pick(genStageNames...))
	}
	sb.WriteString("\n")
	for lines := 0; lines < 12 && !c.done() && c.next(6) != 0; lines++ {
		genInstruction(c, sb)
	}
}

func genInstruction(c *chooser, sb *strings.Builder) {
	switch c.next(14) {
	case 0:
		sb.WriteString("ARG " + c.pick(genArgs...) + c.pick("", "=2", "=$VERSION") + "\n")
	case 1:
		sb.WriteString("ENV " + c.pick(genArgs...) + c.pick("=/app", "=\"two words\"", " legacy value", "=${VERSION:-dev}", "=${VERSION:+v$VERSION}", "='$DEBUG'", "=${APP_HOME}/lib A=b", "=\\$HOME") + "\n")
	case 2:
		sb.WriteString(c.pick("RUN", "RUN", "run") + c.pick("", " --mount=type=cache,target=/root/.cache", " --network=none") + " " + c.pick(genCommands...))
		if c.next(3) == 0 {
			sb.WriteString(" \\\n    && " + c.pick(genCommands...))
		}
		sb.WriteString("\n")
	case 3:
		sb.WriteString("RUN <<" + c.pick("EOF", "-EOF", "\"EOF\"") + "\n" + c.pick(genCommands...) + "\n" + "EOF\n")
	case 4:
		sb.WriteString("COPY" + c.pick("", " --from="+c.pick(genStageNames...), " --chown=1000:1000", " --link") + " " + c.pick(genPaths...) + " " + c.pick(genPaths...) + "\n")
	case 5:
		sb.WriteString("WORKDIR " + c.pick(genPaths...) + c.pick("", "", "/${VERSION-x}", "$1") + "\n")
	case 6:
		sb.WriteString(c.pick("CMD", "ENTRYPOINT") + " " + c.pick("[\"/app\"]", "[\"sh\", \"-c\", \"echo hi\"]", "echo hi", "exec /app --serve") + "\n")
	case 7:
		sb.WriteString("EXPOSE " + c.pick("80", "443/tcp", "8080/UDP", "80:80") + "\n")
	case 8:
		sb.WriteString("LABEL " + c.pick("org.opencontainers.image.version=$VERSION", "maintainer=\"me\"", "description legacy") + "\n")
	case 9:
		sb.WriteString("USER " + c.pick("app", "1000:1000", "$USER_ID") + "\n")
	case 10:
		sb.WriteString(c.pick("SHELL [\"/bin/bash\", \"-o\", \"pipefail\", \"-c\"]", "VOLUME /data", "STOPSIGNAL SIGTERM", "MAINTAINER me") + "\n")
	case 11:
		sb.WriteString("HEALTHCHECK " + c.pick("--interval=30s CMD curl -f http://localhost/ || exit 1", "NONE", "CMD [\"true\"]") + "\n")
	case 12:
		sb.WriteString("ONBUILD " + c.pick("RUN echo onbuild", "COPY . /app") + "\n")
	case 13:
		sb.WriteString(c.pick("# a comment", "", "ADD https://example.com/a.tgz /tmp/", "ADD --chmod=755 run.sh /usr/local/bin/") + "\n")
	}
}
