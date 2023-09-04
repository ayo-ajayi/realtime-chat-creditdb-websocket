# Real-time Chat Application with websockets and [CreditDB](https://github.com/creditdb)

This is a real-time chat application that utilizes WebSocket for instant messaging and [CreditDB](https://github.com/creditdb) for storing conversations. Messages can be sent using HTTP/1.1 POST requests and are delivered to the recipient in real-time using WebSocket. Even if the recipient is offline at the time a message is sent, they can access it when they come online, as messages are stored in a database.


## Features
- Real-time messaging using [Gorilla](https://github.com/gorilla/websocket) WebSocket.
- Message storage in CreditDB.


## License

[MIT](LICENSE)


##  Author

-   [Ayomide Ajayi](https://github.com/ayo-ajayi)