import json
import subprocess


def add_tag(tag, tags=[]):
    tags.append(tag)
    return tags


def save(path, state):
    try:
        with open(path, "w") as file:
            json.dump(state, file)
    except:
        pass


def convert(name):
    subprocess.run("convert " + name + " out.png", shell=True)


def share(total, people):
    return total / people


def safe_share(total, people):
    if people == 0:
        return 0
    return total / people
