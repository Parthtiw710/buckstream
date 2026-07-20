# deploy/garage/Dockerfile
FROM dxflrs/garage:v0.9.0

# Copy our default config into the container
COPY garage.toml /etc/garage.toml

# Expose S3 API and Admin ports
EXPOSE 3900
EXPOSE 3903

# Create the data directory
RUN mkdir -p /data/meta /data/blobs

# Run Garage with the default config
CMD ["/garage", "server", "-config", "/etc/garage.toml"]
