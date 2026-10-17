from alpine as Base
run echo lower
FROM base AS final
COPY --from=Base / /
