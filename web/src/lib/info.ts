export const APP_VERSION = process.env.NEXT_PUBLIC_APP_VERSION || 'unknown';
export const GITHUB_REPO = process.env.NEXT_PUBLIC_GITHUB_REPO || 'https://github.com/mingtian886/octopus';

/**
 * 发布镜像地址，从 GITHUB_REPO 推导，避免仓库改名后此处失去同步。
 * 发布流程只产出 Docker 镜像，升级方式是拉取新镜像后重建容器。
 */
export const DOCKER_IMAGE = `ghcr.io/${GITHUB_REPO.replace(/^https?:\/\/github\.com\//, '').replace(/\/$/, '')}:latest`;
