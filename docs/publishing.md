# Publishing with GitBook

This repository follows GitBook's current Git Sync content configuration:

- `.gitbook.yaml` is at the repository root;
- `root` points to `./docs/`;
- `docs/README.md` is the landing page;
- `docs/SUMMARY.md` is the navigation tree.

To publish:

1. Commit and push the documentation files to the branch GitBook should track.
2. In a GitBook space, enable Git Sync and connect this GitHub repository and branch.
3. Keep the Git Sync project directory at the repository root so GitBook can read `.gitbook.yaml`.
4. Confirm the preview uses `docs/README.md` and `docs/SUMMARY.md`.
5. Publish the GitBook space and configure its public/custom domain settings as required.

GitBook Sync is bi-directional. To avoid duplicate README conflicts, manage the GitBook landing README in the repository rather than creating another one in the GitBook UI.

References: [GitBook content configuration](https://gitbook.com/docs/getting-started/git-sync/content-configuration) and [GitHub/GitLab Sync](https://gitbook.com/docs/getting-started/git-sync).

## Publishing with GitHub Pages

The repository also contains `mkdocs.yml`, `requirements-docs.txt`, and `.github/workflows/docs-pages.yml`. Pushes to `v5` that change documentation build and deploy a MkDocs site through GitHub Actions. The workflow can also be run manually.

GitHub Pages uses the generated `site/` artifact; source Markdown remains in `docs/`. GitBook-specific `.gitbook.yaml` and `SUMMARY.md` remain available without affecting the Pages build.
