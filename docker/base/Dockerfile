FROM docker:dind

# Install nodejs dependencies
RUN apk add \
    curl \
    bash \ 
    binutils-gold \
    g++ \
    gcc \
    gnupg \
    libgcc \
    linux-headers \
    make \
    python3 \
    py-setuptools


SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ENV BASH_ENV=/root/.bash_env
RUN touch "${BASH_ENV}"
RUN echo '. "${BASH_ENV}"' >> ~/.bashrc

# Download and install nvm
RUN curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | PROFILE="${BASH_ENV}" bash
RUN echo node > .nvmrc
RUN nvm install 22

# confirm installation
RUN node -v
RUN npm -v