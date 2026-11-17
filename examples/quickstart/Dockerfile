# Built by the openshift-build resource: `score-ocp` uploads this directory to the BuildConfig on first deploy.
FROM registry.access.redhat.com/ubi9/python-312
# The base image builds as UID 1001; set up as root, run as 1001. OpenShift then runs it under a random UID from
# the namespace range, so nothing here may depend on the UID or write to the image filesystem.
USER 0
WORKDIR /app
COPY app.py /app/app.py
# Each build stamps a fresh id, so a rollout triggered by a new build is visible at /.
RUN date +%s%N > /app/build-id && chmod -R a+rX /app
USER 1001
EXPOSE 8080
CMD ["python", "/app/app.py"]
