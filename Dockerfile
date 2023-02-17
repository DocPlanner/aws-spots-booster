########################################################################################################################
# BASE
########################################################################################################################
FROM debian:buster-slim as base

# Prepare app directory
RUN mkdir -p /usr/app/controller/
WORKDIR /usr/app/controller/

# Configure entrypoint
COPY ./docker-entrypoint.sh /usr/local/bin/
RUN chmod 0775 /usr/local/bin/docker-entrypoint.sh
ENTRYPOINT ["docker-entrypoint.sh"]
# This step will be replaced by the entrypoint plus the args field defined in the kubernetes eployment manifest
CMD ["sh"]

########################################################################################################################
# BUILD
########################################################################################################################
FROM golang:1.19-buster as build

# Copy the application files
COPY . /usr/app/controller/

# Build the application
RUN cd /usr/app/controller/ \
    && make build

########################################################################################################################
# APPLICATION
# FOR PROD BUILD ADD TARGET FLAG: docker build . --tag 'aws-spots-booster:buster-slim' --target application
########################################################################################################################
FROM base as application

# Copy the build application to the working directory
COPY --from=build /usr/app/controller/bin/* /usr/app/controller/

# Prepare executable permissions
RUN chmod -R 0775 /usr/app/controller/aws-spots-booster

# Link application
RUN ln -s /usr/app/controller/aws-spots-booster /usr/local/bin/case && \
    chmod +x /usr/local/bin/aws-spots-booster

########################################################################################################################
# DEBUG
# FOR A DEBUG BUILD: docker build . --tag 'aws-spots-booster:buster-slim'
########################################################################################################################
FROM application as debug
# Install debug packages
RUN apt-get update --yes && \
    apt-get install --yes --no-install-recommends \
        bash \
        procps