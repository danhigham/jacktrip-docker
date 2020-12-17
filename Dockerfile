FROM alpine

RUN apk update
RUN apk upgrade

RUN apk add git g++ make python3 bash linux-headers qt5-qtbase-dev

RUN git clone https://github.com/jackaudio/jack2.git
WORKDIR /jack2
RUN git fetch --tags
RUN latestTag=$(git describe --tags `git rev-list --tags --max-count=1`)
RUN git checkout $latestTag

RUN ./waf configure
RUN ./waf
RUN ./waf install

WORKDIR /

RUN git clone https://github.com/jacktrip/jacktrip.git
WORKDIR /jacktrip
RUN git fetch --tags
RUN latestTag=$(git describe --tags `git rev-list --tags --max-count=1`)
RUN git checkout $latestTag

WORKDIR /jacktrip/src
RUN ./build
RUN cp ../builddir/jacktrip /usr/local/bin/

WORKDIR /usr/bin
COPY start-jacktrip.sh .
RUN chmod +x start-jacktrip.sh

CMD ["start-jacktrip.sh"]

EXPOSE 61002/tcp
EXPOSE 4464/tcp
EXPOSE 4464/udp
