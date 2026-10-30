/*
 * Copyright 2026 CashStratum contributors
 *
 * This program is free software; you can redistribute it and/or modify it
 * under the terms of the GNU General Public License as published by the
 * Free Software Foundation; either version 3 of the License, or (at your
 * option) any later version.
 *
 * sv2-noise-probe: a Stratum V2 Noise handshake initiator built on the
 * reference `noise_sv2` crate. It checks that a CashStratum responder's
 * certificate is accepted by an independent implementation.
 *
 * Usage:
 *   sv2-noise-probe <host> <port> <authority_b58 | stratum2+tcp://host:port/authority_b58>
 *
 * Exit codes:
 *   0  handshake accepted; prints "OK" on stdout
 *   1  certificate rejected; prints the Debug form of InvalidCertificate on stdout
 *   2  any other error; message on stderr
 */
use std::{env, io::{Read, Write}, net::TcpStream, process::exit, str::FromStr, time::Duration};
use key_utils::Secp256k1PublicKey;
use noise_sv2::{Error, Initiator, INITIATOR_EXPECTED_HANDSHAKE_MESSAGE_SIZE as ACT2};

fn main() {
    let a: Vec<String> = env::args().collect();
    if a.len() != 4 {
        eprintln!("usage: {} host port <authority_b58 | stratum2+tcp://host:port/authority_b58>", a[0]);
        exit(2);
    }
    let key_str = a[3].rsplit('/').next().unwrap(); // strip optional URL prefix
    let pk = match Secp256k1PublicKey::from_str(key_str) {
        Ok(k) => k,
        Err(e) => { eprintln!("bad authority key: {e}"); exit(2); }
    };
    // from_raw_k(32 bytes) -> Result<Box<Initiator>, Error>; avoids secp256k1 type mismatches
    let mut init = match Initiator::from_raw_k(pk.into_bytes()) {
        Ok(i) => i,
        Err(e) => { eprintln!("raw key: {e:?}"); exit(2); }
    };
    let mut s = match TcpStream::connect(format!("{}:{}", a[1], a[2])) {
        Ok(s) => s,
        Err(e) => { eprintln!("connect: {e}"); exit(2); }
    };
    s.set_read_timeout(Some(Duration::from_secs(10))).ok();
    let act1 = match init.step_0() { // [u8; 64]
        Ok(m) => m,
        Err(e) => { eprintln!("step_0: {e:?}"); exit(2); }
    };
    if let Err(e) = s.write_all(&act1) { eprintln!("write act1: {e}"); exit(2); }
    let mut act2 = [0u8; ACT2]; // 234 bytes, raw (no frame header)
    if let Err(e) = s.read_exact(&mut act2) { eprintln!("read act2: {e}"); exit(2); }
    match init.step_2(act2) { // verifies cert version + validity + signature vs authority key
        Ok(_engine) => { println!("OK"); }
        Err(e @ Error::InvalidCertificate(_)) => { println!("{e:?}"); exit(1); }
        Err(e) => { eprintln!("{e:?}"); exit(2); }
    }
}
