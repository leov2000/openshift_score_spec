# The image is built from the Pixi bundle (`make image`), not from source: the build context is the folder holding
# fastapi-hello-env.tar.gz. The bundle carries its own Python, so ADD extracting it is all the image needs: no Python,
# pixi or pip installed in it.
FROM docker.io/library/debian:bookworm-slim
ADD fastapi-hello-env.tar.gz /opt/app/env
USER nobody
ENV PORT=8000
EXPOSE 8000
# Run the console script with the bundle's own Python: its first line holds the build folder's path.
ENTRYPOINT ["/opt/app/env/bin/python3.13", "/opt/app/env/bin/fastapi-hello"]
