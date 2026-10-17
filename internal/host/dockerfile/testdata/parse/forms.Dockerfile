FROM alpine
CMD ['echo']
ENTRYPOINT ["a" , "b"]
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
VOLUME ["/data", "/logs"]
VOLUME /cache /tmp
HEALTHCHECK --interval=5s --retries=3 CMD curl -f http://localhost/ || exit 1
HEALTHCHECK CMD ["true"]
HEALTHCHECK NONE
ONBUILD RUN echo hi
ONBUILD COPY . /app
