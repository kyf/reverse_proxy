FROM busybox

RUN mkdir /proxy

ADD ./reverse_proxy /proxy/
ADD ./cert /proxy/cert
ADD ./conf /proxy/conf

EXPOSE 443

ENV ROOT_DIR /proxy

ENTRYPOINT /proxy/reverse_proxy 
