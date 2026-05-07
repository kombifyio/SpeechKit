```python
import os, requests

with open("hello.wav", "rb") as audio:
    response = requests.post(
        os.environ["SPEECHKIT_SERVER_URL"] + "/v1/dictation/transcribe",
        headers={"Authorization": "Bearer " + os.environ["SPEECHKIT_TOKEN"]},
        files={"audio": audio},
        data={"language": "en"},
        timeout=60,
    )
response.raise_for_status()
print(response.json()["text"])
```
