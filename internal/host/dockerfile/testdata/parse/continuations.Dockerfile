FROM alpine
RUN echo a \  
  b
RUN echo c \
# a comment inside
  d
RUN echo e \

  f
LABEL a=b \
      c=d
