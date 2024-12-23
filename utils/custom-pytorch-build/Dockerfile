ARG BASE_IMAGE=ubuntu:22.04
ARG PYTHON_VERSION=3.11

FROM ${BASE_IMAGE} AS dev-base
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y \
        build-essential \
        ca-certificates \
        ccache \
        cmake \
        curl \
        git \
        libjpeg-dev \
        libcurl4-openssl-dev \
        libpng-dev && \
    rm -rf /var/lib/apt/lists/*
RUN /usr/sbin/update-ccache-symlinks
RUN mkdir /opt/ccache && ccache --set-config=cache_dir=/opt/ccache
ENV PATH /opt/conda/bin:$PATH

FROM dev-base AS conda
ARG PYTHON_VERSION=3.11
# Automatically set by buildx
ARG TARGETPLATFORM
# translating Docker's TARGETPLATFORM into miniconda arches
WORKDIR /opt
RUN git clone https://github.com/lilbillybiscuit/pytorch-c4d.git /opt/pytorch
WORKDIR /opt/pytorch
RUN case ${TARGETPLATFORM} in \
         "linux/arm64")  MINICONDA_ARCH=aarch64  ;; \
         *)              MINICONDA_ARCH=x86_64   ;; \
    esac && \
    curl -fsSL -v -o ~/miniconda.sh -O  "https://github.com/conda-forge/miniforge/releases/latest/download/Miniforge3-Linux-${MINICONDA_ARCH}.sh"

# Manually invoke bash on miniconda script per https://github.com/conda/conda/issues/10431
RUN chmod +x ~/miniconda.sh && \
    bash ~/miniconda.sh -b -p /opt/conda && \
    rm ~/miniconda.sh && \
    /opt/conda/bin/conda install -y python=${PYTHON_VERSION} cmake conda-build pyyaml numpy ipython && \
    /opt/conda/bin/python -mpip install -r requirements.txt && \
    /opt/conda/bin/conda clean -ya

FROM conda as submodule-update
WORKDIR /opt/pytorch
RUN git submodule update --init --recursive


FROM submodule-update as conda-installs
ARG PYTHON_VERSION=3.11
ARG CUDA_PATH=cu121
ARG CUDA_CHANNEL=nvidia
ARG INSTALL_CHANNEL=whl/nightly
# Automatically set by buildx
RUN /opt/conda/bin/conda update -y -n base -c defaults conda
RUN /opt/conda/bin/conda install -y python=${PYTHON_VERSION}

ARG TARGETPLATFORM

# INSTALL_CHANNEL whl - release, whl/nightly - nightly, whle/test - test channels
RUN case ${TARGETPLATFORM} in \
         "linux/arm64")  pip install --extra-index-url https://download.pytorch.org/whl/cpu/ torch torchvision torchaudio ;; \
         *)              pip install --index-url https://download.pytorch.org/${INSTALL_CHANNEL}/${CUDA_PATH#.}/ torch torchvision torchaudio ;; \
    esac && \
    /opt/conda/bin/conda clean -ya
RUN /opt/conda/bin/pip install torchelastic
RUN IS_CUDA=$(python -c 'import torch ; print(torch.cuda._is_compiled())'); \
    echo "Is torch compiled with cuda: ${IS_CUDA}"; \
    if test "${IS_CUDA}" != "True" -a ! -z "${CUDA_VERSION}"; then \
        exit 1; \
    fi


FROM submodule-update as builder
ARG CMAKE_VARS
WORKDIR /opt/pytorch
COPY --from=conda /opt/conda /opt/conda
COPY --from=submodule-update /opt/pytorch /opt/pytorch
RUN git checkout main
RUN git pull
RUN git submodule update --init --recursive
#RUN pip install pytorch-triton
RUN pip uninstall -y triton pytorch-triton
RUN --mount=type=cache,target=/opt/ccache \
    export eval ${CMAKE_VARS} && \
    CMAKE_PREFIX_PATH="$(dirname $(which conda))/../" \
    python setup.py install

FROM builder as builder-update
ARG CMAKE_VARS
ARG CACHEBUST=1
WORKDIR /opt/pytorch

RUN git pull
RUN git submodule update --init --recursive
RUN --mount=type=cache,target=/opt/ccache \
    export eval ${CMAKE_VARS} && \
    CMAKE_PREFIX_PATH="$(dirname $(which conda))/../" \
    python setup.py install

# Stage 2: Final slim image
FROM ubuntu:22.04 as official

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates \
        libjpeg-dev \
        libpng-dev \
        libcurl4-openssl-dev \
        && rm -rf /var/lib/apt/lists/*

# Copy from previous builds
COPY --from=builder-update /opt/conda /opt/conda
RUN if test -n "${TRITON_VERSION}" -a "${TARGETPLATFORM}" != "linux/arm64"; then \
        DEBIAN_FRONTEND=noninteractive apt install -y --no-install-recommends gcc; \
        rm -rf /var/lib/apt/lists/*; \
    fi
ENV PATH /opt/conda/bin:$PATH
WORKDIR /workspace
