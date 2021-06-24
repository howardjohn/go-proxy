use std::{convert::Infallible, sync::Mutex};

use hyper::service::{make_service_fn, service_fn};
use hyper::{Body, Client, Request, Response, Server};
use std::sync::atomic::{AtomicU64, Ordering};
use std::{sync::Arc, time::Instant};

static REQUESTS: AtomicU64 = AtomicU64::new(0);

struct ReverseProxy {
    uri: hyper::Uri,
    client: Client<hyper::client::HttpConnector>,
}

impl ReverseProxy {
    async fn handle(&self) -> Result<Response<Body>, hyper::Error> {
        self.client.get(self.uri.clone()).await
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let ports_s: String = std::env::var("PORT").unwrap_or("15006".to_string());
    let ports = ports_s.split(",");

    let mut v = Vec::new();
    for port in ports {
        let addr: std::net::SocketAddr = ("[::]:".to_owned() + port).parse()?;
        println!("Listening on http://{}", addr);
        let uri = hyper::Uri::from_static("http://localhost:8080");
        let client = Client::new();

        let rp = std::sync::Arc::new(ReverseProxy { client, uri: uri });
        let first_request_time: Arc<Mutex<Instant>> = Arc::new(Mutex::new(Instant::now()));
        let service = make_service_fn(move |_| {
            let frt = first_request_time.clone();
            let rp = rp.clone();
            async move {
                Ok::<_, Infallible>(service_fn(move |_req: Request<Body>| {
                    let rp = rp.clone();
                    let rc = REQUESTS.fetch_add(1, Ordering::Relaxed);
                    if rc == 0 {
                        let mut frt2 = frt.lock().unwrap();
                        *frt2 = Instant::now();
                        println!("Completed first request");
                    } else if rc % 10000 == 0 {
                        println!(
                            "Completed request {}, rate is {:?}",
                            rc,
                            rc as f64 / frt.lock().unwrap().elapsed().as_secs_f64()
                        );
                    }
                    async move { rp.handle().await }
                }))
            }
        });
        let server = Server::bind(&addr).serve(service);
        v.push(server);
    }
    futures::future::join_all(v).await;

    Ok(())
}
