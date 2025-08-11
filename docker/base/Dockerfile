FROM debian:12-slim

# Install base dependencies
RUN set -eux; \
  apt-get update; \
  apt-get install -y --no-install-recommends \
  build-essential \
  ca-certificates \
  curl \
  git \
  gnupg \
  iptables \
  net-tools \
  openssl \
  pigz \
  python3 \
  sudo \
  unzip \
  xz-utils \
  ; \
  rm -rf /var/lib/apt/lists/*

RUN update-alternatives --set iptables /usr/sbin/iptables-legacy
RUN update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy

# Add apt repository for docker
RUN  apt-get update && \
  apt-get install ca-certificates curl && \
  install -m 0755 -d /etc/apt/keyrings && \
  curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc && \
  chmod a+r /etc/apt/keyrings/docker.asc && \
  echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  sudo tee /etc/apt/sources.list.d/docker.list > /dev/null && \
  apt-get update

# Install docker
RUN apt-get install -y --no-install-recommends docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Install Node.js
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
  apt-get install -y nodejs

# Enable corepack
RUN corepack enable

# Enable yarn
RUN corepack prepare yarn@stable --activate

# Enable pnpm
ENV PNPM_HOME="/pnpm"
RUN corepack prepare pnpm@latest --activate
# Add pnpm to PATH
ENV PATH="${PNPM_HOME}:${PATH}"

# Install Bun
RUN curl -fsSL https://bun.sh/install | bash
# Add Bun to PATH
ENV PATH="/root/.bun/bin:${PATH}"

# Install python 3.11
RUN apt-get update && apt-get install -y --no-install-recommends \
  python3.11 \
  python3.11-dev \
  python3-pip \
  && rm -rf /var/lib/apt/lists/*

# Install rust
RUN curl https://sh.rustup.rs -sSf | bash -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Install claude code
RUN pnpm add -g @anthropic-ai/claude-code