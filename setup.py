from setuptools import setup, find_packages

setup(
    name="c4d-research",
    version="0.1",
    packages=find_packages(),
    install_requires=[
        "numpy",
        "pandas",
        "pytorch",
        "kubernetes",
    ],
)
