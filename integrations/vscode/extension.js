'use strict';

const fs = require('fs');
const path = require('path');
const vscode = require('vscode');

const stateDirectory = path.join(process.env.LOCALAPPDATA || '', 'Metuur');
const statePath = path.join(stateDirectory, 'vscode-active-file.json');
const heartbeatIntervalMs = 5000;

let writeQueue = Promise.resolve();
let heartbeatTimer;
let lastGoState;

function stateForDocument(document) {
  if (!process.env.LOCALAPPDATA || !document || document.uri.scheme !== 'file') {
    return undefined;
  }

  const filePath = document.uri.fsPath;
  if (document.languageId !== 'go' || path.extname(filePath).toLowerCase() !== '.go') {
    return undefined;
  }

  const folder = vscode.workspace.getWorkspaceFolder(document.uri);
  const workspace = folder && folder.uri.scheme === 'file' ? folder.uri.fsPath : '';
  return {
    path: filePath,
    workspace,
    updatedAt: new Date().toISOString()
  };
}

async function writeStateAtomic(state) {
  await fs.promises.mkdir(stateDirectory, { recursive: true });

  const temporaryPath = `${statePath}.${process.pid}.${Date.now()}.tmp`;
  const contents = `${JSON.stringify(state, null, 2)}\n`;
  try {
    await fs.promises.writeFile(temporaryPath, contents, {
      encoding: 'utf8',
      mode: 0o600,
      flag: 'wx'
    });
    await fs.promises.rename(temporaryPath, statePath);
  } finally {
    await fs.promises.rm(temporaryPath, { force: true }).catch(() => {});
  }
}

async function clearState() {
  await fs.promises.rm(statePath, { force: true });
}

function publishDocument(document) {
  if (!process.env.LOCALAPPDATA) {
    return;
  }

  let state;
  if (!document) {
    if (!lastGoState) {
      return;
    }
    state = { ...lastGoState, updatedAt: new Date().toISOString() };
  } else {
    state = stateForDocument(document);
    lastGoState = state ? { path: state.path, workspace: state.workspace } : undefined;
  }

  writeQueue = writeQueue
    .then(() => (state ? writeStateAtomic(state) : clearState()))
    .catch((error) => {
      console.error('[Metuur VS Code Bridge] Cannot update active Go file state:', error);
    });
}

function publishActiveEditor() {
  const editor = vscode.window.activeTextEditor;
  publishDocument(editor ? editor.document : undefined);
}

function stopHeartbeat() {
  if (heartbeatTimer) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = undefined;
  }
}

function activate(context) {
  publishActiveEditor();
  stopHeartbeat();
  heartbeatTimer = setInterval(publishActiveEditor, heartbeatIntervalMs);

  context.subscriptions.push(
    { dispose: stopHeartbeat },
    vscode.window.onDidChangeActiveTextEditor((editor) => {
      publishDocument(editor ? editor.document : undefined);
    }),
    vscode.workspace.onDidSaveTextDocument((document) => {
      const active = vscode.window.activeTextEditor;
      if (active && active.document.uri.toString() === document.uri.toString()) {
        publishDocument(document);
      }
    }),
    vscode.workspace.onDidChangeWorkspaceFolders(() => publishActiveEditor())
  );
}

function deactivate() {
  stopHeartbeat();
  return writeQueue;
}

module.exports = {
  activate,
  deactivate
};
