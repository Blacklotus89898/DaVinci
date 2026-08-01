# Pod Stuck in ImagePullBackOff / ErrImagePull

## Symptom
`kubectl get pods` shows `ImagePullBackOff` or `ErrImagePull`. Pod never starts.

## Root Cause
Kubelet can't pull the container image — usually a wrong tag/typo, the image was never pushed, the tag was deleted from the registry, or missing/expired registry credentials (`imagePullSecrets`).

## Fix
```bash
# see the exact pull error
kubectl describe pod <pod> -n <ns> | grep -A5 'Failed to pull image'

# confirm the image exists and is spelled correctly
docker pull <image>:<tag>

# check the pull secret is attached and not expired
kubectl get sa <serviceaccount> -n <ns> -o yaml | grep imagePullSecrets
kubectl get secret <pull-secret> -n <ns> -o yaml
```

## Prevention
- Never deploy `:latest` in production — pin immutable tags/digests so a retag elsewhere can't break a running rollout.
- Rotate registry credentials before they expire and alert on pull-secret age.
- Add a CI step that verifies the image tag exists in the registry before applying the manifest.
