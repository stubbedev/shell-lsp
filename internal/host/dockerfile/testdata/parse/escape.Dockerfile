# escape=`
FROM mcr.microsoft.com/windows/servercore
RUN echo a `
  b
COPY C:\src C:\dst
