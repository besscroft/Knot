// 按 CRX3 规范打包插件（与 Chromium crx_creator.cc 同一结构）：
//   Cr24 + LE32(3) + LE32(header) + CrxFileHeader + zip
// CrxFileHeader = signed_header_data(SignedData{crx_id}) + sha256_with_rsa 证明
// 签名 = RSA-SHA256 PKCS1v15 over "CRX3 SignedData\0" || LE32(signed 长度)
//        || signed_header_data || zip 原文
// 产物自校验（回读验签 + 比对 ID），随后把 crx/zip/meta 同步到
// internal/service/extbundle/（供 go:embed 与浏览器注册表安装）。
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const extRoot = path.resolve(here, '..');
const outDir = path.join(extRoot, '.output');
const bundleDir = path.resolve(extRoot, '..', 'internal', 'service', 'extbundle');

// ---- protobuf 手工编码（消息都很小，无需依赖库） ----
const varint = (n) => {
  const out = [];
  let v = n;
  while (v > 0x7f) {
    out.push((v & 0x7f) | 0x80);
    v >>>= 7;
  }
  out.push(v);
  return Buffer.from(out);
};
const field = (tag, payload) =>
  Buffer.concat([varint((tag << 3) | 2), varint(payload.length), payload]);

// SignedData{ crx_id }：field 1
const signedData = (crxId) => field(1, crxId);
// AsymmetricKeyProof{ public_key, signature }：field 1 + field 2
const keyProof = (pub, sig) => Buffer.concat([field(1, pub), field(2, sig)]);
// CrxFileHeader：field 10000 = signed_header_data，field 2 = sha256_with_rsa
// （字段号来自 components/crx_file/crx3.proto，勿按直觉写反）
const crxHeader = (signed, proof) => Buffer.concat([field(10000, signed), field(2, proof)]);

function main() {
  const manifest = JSON.parse(
    fs.readFileSync(path.join(outDir, 'chrome-mv3', 'manifest.json'), 'utf8'),
  );
  const version = manifest.version;

  const zipCandidates = fs
    .readdirSync(outDir)
    .filter((f) => f.endsWith('-chrome.zip'))
    .map((f) => ({ f, m: fs.statSync(path.join(outDir, f)).mtimeMs }))
    .sort((a, b) => b.m - a.m);
  if (!zipCandidates.length) throw new Error('未找到 wxt zip 产物，请先运行 wxt zip');
  const zipPath = path.join(outDir, zipCandidates[0].f);
  const zip = fs.readFileSync(zipPath);

  const keyPem = fs.readFileSync(path.join(extRoot, 'extension-key.pem'), 'utf8');
  const privateKey = crypto.createPrivateKey(keyPem);
  const publicKey = crypto.createPublicKey(keyPem);
  const spkiDer = publicKey.export({ type: 'spki', format: 'der' });

  // crx_id = SHA256(SPKI) 前 16 字节（id_util::GenerateId 的输入）
  const crxId = crypto.createHash('sha256').update(spkiDer).digest().subarray(0, 16);
  let id = '';
  for (const b of crxId) id += ((b >> 4) + 10).toString(36) + ((b & 15) + 10).toString(36);

  const signed = signedData(crxId);
  const context = Buffer.from('CRX3 SignedData\0', 'binary');
  const sizeOctets = Buffer.alloc(4);
  sizeOctets.writeUInt32LE(signed.length);
  const signature = crypto.sign('sha256', Buffer.concat([context, sizeOctets, signed, zip]), {
    key: privateKey,
    padding: crypto.constants.RSA_PKCS1_PADDING,
  });

  const header = crxHeader(signed, keyProof(spkiDer, signature));
  const magic = Buffer.from([0x43, 0x72, 0x32, 0x34]); // "Cr24"
  const prefix = Buffer.alloc(8);
  prefix.writeUInt32LE(3, 0); // version
  prefix.writeUInt32LE(header.length, 4);
  const crx = Buffer.concat([magic, prefix, header, zip]);

  // ---- 自校验：回读结构并验签（与写入不同的代码路径，防 varint 编码错误） ----
  if (crx.subarray(0, 4).toString('binary') !== 'Cr24') throw new Error('magic 不符');
  if (crx.readUInt32LE(4) !== 3) throw new Error('version 不符');
  const hLen = crx.readUInt32LE(8);
  const headerBytes = crx.subarray(12, 12 + hLen);
  const readFieldAt = (buf, pos) => {
    let tag = 0;
    let shift = 0;
    for (;;) {
      const b = buf[pos.p++];
      tag |= (b & 0x7f) << shift;
      shift += 7;
      if (!(b & 0x80)) break;
    }
    let len = 0;
    shift = 0;
    for (;;) {
      const b = buf[pos.p++];
      len |= (b & 0x7f) << shift;
      shift += 7;
      if (!(b & 0x80)) break;
    }
    return { tag: tag >>> 3, body: buf.subarray(pos.p, (pos.p += len)) };
  };
  const hpos = { p: 0 };
  const fSigned = readFieldAt(headerBytes, hpos);
  const fProof = readFieldAt(headerBytes, hpos);
  if (hpos.p !== headerBytes.length) throw new Error('头部有未消费字节');
  const ipos = { p: 0 };
  const proofPub = readFieldAt(fProof.body, ipos).body;
  const proofSig = readFieldAt(fProof.body, ipos).body;
  if (ipos.p !== fProof.body.length) throw new Error('证明有未消费字节');
  if (fSigned.tag !== 10000 || fProof.tag !== 2) throw new Error('字段号不符');
  const archive = crx.subarray(12 + hLen);
  const verified = crypto.verify(
    'sha256',
    Buffer.concat([context, sizeOctets, fSigned.body, archive]),
    { key: publicKey, padding: crypto.constants.RSA_PKCS1_PADDING },
    proofSig,
  );
  if (!verified) throw new Error('自验签失败');
  if (Buffer.compare(proofPub, spkiDer) !== 0) throw new Error('公钥不一致');

  // ---- 落盘：crx / zip / meta 同步到 extbundle ----
  fs.mkdirSync(bundleDir, { recursive: true });
  fs.writeFileSync(path.join(bundleDir, 'knot-extension.crx'), crx);
  fs.copyFileSync(zipPath, path.join(bundleDir, 'knot-extension.zip'));
  fs.writeFileSync(
    path.join(bundleDir, 'meta.json'),
    JSON.stringify({ id, version }, null, 2) + '\n',
  );
  fs.writeFileSync(path.join(outDir, 'knot-extension.crx'), crx);
  console.log(`crx3 打包完成: id=${id} version=${version} bytes=${crx.length}（验签通过）`);
}

main();
