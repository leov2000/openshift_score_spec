# Both build secrets are mounted read-only for RUN steps only; they are never part of an image layer.
FROM registry.access.redhat.com/ubi9/python-312
RUN test -s /build-secrets/npmrc/.npmrc && echo "build secret npmrc: mounted at /build-secrets/npmrc"
RUN test -s /etc/pki/build-certs/ca.crt && echo "build secret certs: mounted at /etc/pki/build-certs"
EXPOSE 8080
CMD ["python", "-m", "http.server", "8080"]
